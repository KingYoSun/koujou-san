package clientops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/jackc/pgx/v5"
)

type serverModelRequest struct {
	Client string          `json:"client"`
	Turn   Ref             `json:"turn"`
	Body   json.RawMessage `json:"body"`
}
type serverExchange struct {
	Turn     Ref                `json:"turn"`
	State    string             `json:"state"`
	Request  json.RawMessage    `json:"request"`
	Response serverllm.Response `json:"response"`
	Failure  string             `json:"failure,omitempty"`
}
type serverObservation struct {
	Ref
	Protocol, Digest string
}
type serverModelReply struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func (s *Service) serverConnection(w string, model Model) (serverllm.Connection, error) {
	connection, err := s.Gateway.Connection(w, model.ServerConnection, model.Revision)
	if err != nil || model.ServerConnection == "" || model.Connection != "server:"+model.ServerConnection+":"+model.Requested || !connection.Allows(model.Requested, model.Allowed) {
		return serverllm.Connection{}, ErrInvalid
	}
	return connection, nil
}

func (s *Service) resolveServerModel(w string, model *Model) error {
	if model.ServerConnection == "" {
		return nil
	}
	connection, err := s.serverConnection(w, *model)
	if err == nil {
		model.Endpoint = connection.Endpoint
	}
	return err
}

// ServerModel dispatches one already-reserved turn. Its persistent receipt is
// committed before external I/O, and an unknown dispatch is never replayed.
func (s *Service) ServerModel(ctx context.Context, p access.Principal, w, id string, req serverModelRequest) (serverModelReply, error) {
	empty := serverModelReply{}
	owned, err := s.Read(ctx, p, w, Ref{ID: id, Version: "head"})
	var op Operation
	if err != nil || decode(owned.Value, &op) != nil || op.User != p.UserID || op.Client != req.Client || op.Turn == nil || op.Turn.ID != req.Turn.ID {
		return empty, access.ErrForbidden
	}
	intent, err := s.Read(ctx, p, w, req.Turn)
	var turn Turn
	if err != nil || decode(intent.Value, &turn) != nil || turn.State != "intent" || turn.Client != req.Client || turn.Model.ServerConnection == "" {
		return empty, ErrState
	}
	sum := sha256.Sum256(req.Body)
	if hex.EncodeToString(sum[:]) != turn.InputDigest {
		return empty, ErrInvalid
	}
	var request struct {
		Model string `json:"model"`
		Tools []struct {
			Type     string
			Function struct{ Name string }
		} `json:"tools"`
	}
	if json.Unmarshal(req.Body, &request) != nil || request.Model != turn.Model.Requested {
		return empty, ErrInvalid
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(req.Body, &fields)
	for key := range fields {
		switch key {
		case "model", "messages", "stream", "tools", "tool_choice", "response_format":
		default:
			return empty, ErrInvalid
		}
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" || !ReadTool(tool.Function.Name, op.Purpose) {
			return empty, ErrInvalid
		}
	}
	connection, err := s.serverConnection(w, turn.Model)
	if err != nil {
		return empty, err
	}
	claimed := false
	var deadline time.Time
	exchangeID := "client-server-dispatch:" + req.Turn.ID
	claim, err := accepted(s.mutate(ctx, p, w, exchangeID, apicontract.Mutation{OperationID: "dispatch:" + req.Turn.ID, ExpectedVersion: "0"}, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		current, live, err := s.owner(ctx, tx, p, w, id)
		if err != nil {
			return Record{}, err
		}
		if live.State != "sending" || live.Client != req.Client || live.Turn == nil || *live.Turn != req.Turn || hash(live.Model) != hash(turn.Model) {
			return Record{}, ErrState
		}
		policy, err := s.currentPolicy(ctx, tx, p, w, live)
		if err != nil {
			return Record{}, err
		}
		if err := s.modelSources(ctx, tx, p, w, live, policy); err != nil {
			return Record{}, err
		}
		for _, tool := range request.Tools {
			if policy.Fixed.Mode == "none" && strings.HasPrefix(tool.Function.Name, "context_") {
				return Record{}, ErrInvalid
			}
		}
		if err := s.checkRefs(ctx, tx, p, w, intent.Refs); err != nil {
			return Record{}, err
		}
		contextRefs := []Ref{}
		for _, ref := range intent.Refs {
			if strings.HasPrefix(ref.ID, "context:") {
				contextRefs = append(contextRefs, ref)
			}
		}
		if err := s.checkRefs(ctx, tx, access.Principal{UserID: p.UserID, OperationID: live.ReadOperation}, w, contextRefs); err != nil {
			return Record{}, err
		}
		if err := tx.QueryRow(ctx, `SELECT deadline FROM client_operation_heads WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&deadline); err != nil {
			return Record{}, err
		}
		if !time.Now().Before(deadline) {
			return Record{}, ErrState
		}
		claimed = true
		return s.save(ctx, tx, w, p, ref, "server-model-dispatch", serverExchange{Turn: req.Turn, State: "reserved", Request: req.Body}, unique(append(append([]Ref{req.Turn}, current.Refs...), intent.Refs...)), id)
	}))
	if err != nil {
		return empty, err
	}
	if claimed {
		call, cancel := context.WithDeadline(ctx, deadline)
		response, callErr := connection.Call(call, req.Body, turn.RequestID)
		cancel()
		failure := response.Error
		if response.Terminal == "response.failed" {
			failure = "model_request_failed"
		}
		if response.Reply == nil && failure == "incomplete_model_response" {
			failure = "model_connection_failed"
		}
		if callErr != nil || response.Status != 200 {
			failure = "model_connection_failed"
		}
		observed := serverExchange{Turn: req.Turn, State: "observed", Request: req.Body, Response: response, Failure: failure}
		// Saving the external observation is independent of cancellation or a
		// subsequent permission change; business adoption still rechecks both.
		save, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer stop()
		out, err := s.Store.Apply(save, persistence.Scope{ActorID: "server-model:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: w, ResourceID: exchangeID, Kind: "client-operation", Mutation: apicontract.Mutation{OperationID: "observed:" + req.Turn.ID, ExpectedVersion: claim.Version}, Payload: raw(observed)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
			previous, err := record(save, tx, w, claim)
			if err != nil {
				return nil, err
			}
			saved, err := s.save(save, tx, w, p, Ref{ID: ref.ID, Version: ref.Version}, "server-model-dispatch", observed, previous.Refs, id)
			return raw(saved), err
		})
		if out.Error != nil || out.Data == nil {
			return empty, ErrState
		}
		if err != nil {
			return empty, err
		}
	}
	saved, err := s.Read(context.WithoutCancel(ctx), p, w, Ref{ID: exchangeID, Version: "head"})
	var observed serverExchange
	if err != nil {
		return empty, err
	}
	if decode(saved.Value, &observed) != nil || observed.State != "observed" {
		return empty, ErrState
	}
	body := observed.Response.Body
	if observed.Response.Reply != nil {
		body = raw(observed.Response.Reply)
	}
	live, err := s.Read(context.WithoutCancel(ctx), p, w, Ref{ID: id, Version: "head"})
	if err != nil || decode(live.Value, &op) != nil {
		return empty, ErrState
	}
	if op.Client != req.Client || hash(op.Model) != hash(turn.Model) {
		return empty, ErrState
	}
	if op.State == "sending" && op.Turn != nil && *op.Turn == req.Turn {
		finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer stop()
		_, err = accepted(s.Finish(finish, p, w, id, Finish{Mutation: apicontract.Mutation{OperationID: "server-finish:" + req.Turn.ID, ExpectedVersion: live.Version}, Client: req.Client, Turn: req.Turn, Response: body, Status: observed.Response.Status, Failure: observed.Failure, server: &serverObservation{Ref: saved.Ref, Protocol: observed.Response.Protocol, Digest: hash(observed)}}))
		if err != nil {
			return empty, err
		}
	}
	return serverModelReply{Status: observed.Response.Status, Body: body}, nil
}

func (s *Service) registerServerModels(mux *http.ServeMux) {
	mux.HandleFunc("POST /client-operations/workspaces/{workspace}/operations/{id}/model", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req serverModelRequest
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		value, err := s.ServerModel(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, value, err)
	})
}
