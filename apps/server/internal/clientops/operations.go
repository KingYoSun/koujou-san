package clientops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

func hash(v any) string { b := sha256.Sum256(raw(v)); return hex.EncodeToString(b[:]) }
func accepted(v apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if v.Error != nil || v.Data == nil {
		return Ref{}, ErrState
	}
	return Ref{ID: v.Data.ID, Version: v.Data.Version}, nil
}
func validModel(v Model) bool {
	return v.Connection != "" && v.Revision != "" && v.Requested != "" && len(v.Allowed) > 0 && (v.Cost == nil || usage.ValidCostBudget(*v.Cost))
}
func budgets(p Policy, m Model) []usage.CostBudget {
	out := []usage.CostBudget{}
	if p.Limits.Cost != nil {
		out = append(out, *p.Limits.Cost)
	}
	if m.Cost != nil && (len(out) == 0 || out[0] != *m.Cost) {
		out = append(out, *m.Cost)
	}
	return out
}
func (s *Service) policy(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Policy, error) {
	var v Policy
	r, err := record(ctx, q, w, ref)
	if err != nil {
		return v, err
	}
	if r.Kind != "policy" || !pinned(ref) {
		return v, ErrInvalid
	}
	if err = s.checkRefs(ctx, q, p, w, append(append([]Ref{}, r.Refs...), r.Ref)); err != nil {
		return v, err
	}
	return v, decode(r.Value, &v)
}

type Start struct {
	apicontract.Mutation
	Policy  Ref    `json:"policy"`
	Target  Ref    `json:"target"`
	Goal    string `json:"goal"`
	Purpose string `json:"purpose"`
	Model   Model  `json:"model"`
	Client  string `json:"client"`
}

func (s *Service) Start(ctx context.Context, p access.Principal, w, id string, req Start) (apicontract.Response[apicontract.Reference], error) {
	if !strings.HasPrefix(id, "client-operation:") || !strings.HasPrefix(req.Target.ID, "client-selection:") || req.Target.Version == "" || req.Target.Version == "head" || req.Goal == "" || !validModel(req.Model) || (req.Purpose != "selection" && req.Purpose != "inspection") || p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		policy, err := s.policy(ctx, tx, p, w, req.Policy)
		if err != nil {
			return Record{}, err
		}
		if policy.Mode == "manual" {
			return Record{}, ErrState
		}
		if req.Target.Version != "0" {
			v, err := record(ctx, tx, w, req.Target)
			if err != nil {
				return Record{}, err
			}
			if err = s.checkRefs(ctx, tx, p, w, append(append([]Ref{}, v.Refs...), v.Ref)); err != nil {
				return Record{}, err
			}
		}
		op := Operation{Policy: req.Policy, Target: req.Target, Goal: req.Goal, Purpose: req.Purpose, Model: req.Model, User: p.UserID, Client: req.Client, State: "ready", ReadOperation: "selection:" + hash(id), CandidateOperation: "selection-candidates:" + hash(id)}
		var since time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&since); err != nil {
			return Record{}, err
		}
		op.EvidenceSince = &since
		if req.Client == "" {
			op.State = "waiting"
		}
		for _, o := range []struct {
			id      string
			bounded bool
		}{{op.ReadOperation, policy.Fixed.Mode == "none" || len(policy.Scopes) > 0}, {op.CandidateOperation, policy.Fixed.Mode == "none"}} {
			if _, err = accepted(s.Access.CreateOperation(ctx, p, w, o.id, "read", o.bounded, refs(policy.Fixed), apicontract.Mutation{OperationID: "client-read:" + o.id, ExpectedVersion: "0"}, tx)); err != nil {
				return Record{}, err
			}
		}
		for _, scope := range policy.Scopes {
			if _, err = tx.Exec(ctx, `INSERT INTO access_operation_scopes VALUES($1,$2,$3)`, w, op.ReadOperation, scope); err != nil {
				return Record{}, err
			}
		}
		for _, excluded := range policy.Fixed.Exclude {
			resolved := s.Contexts.InTransaction(tx).Preview(ctx, p, w, contexts.ResolveRequest{Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{excluded}}})
			if !resolved.Ready {
				return Record{}, ErrInvalid
			}
			for _, o := range []string{op.ReadOperation, op.CandidateOperation} {
				ids := []string{}
				for _, placement := range resolved.Manifest.Placements {
					ids = append(ids, resource(placement.Ref))
				}
				if _, err = tx.Exec(ctx, `INSERT INTO access_operation_exclusions SELECT $1,$2,value FROM jsonb_array_elements_text($3::jsonb) ON CONFLICT DO NOTHING`, w, o, raw(ids)); err != nil {
					return Record{}, err
				}
			}
		}
		dependencies := append([]Ref{req.Policy}, refs(policy.Fixed)...)
		for _, price := range req.Model.Prices {
			dependencies = append(dependencies, Ref{ID: price.ID, Version: price.Version})
		}
		if err = s.checkRefs(ctx, tx, p, w, dependencies); err != nil {
			return Record{}, err
		}
		if req.Target.Version != "0" {
			dependencies = append(dependencies, req.Target)
		}
		return s.save(ctx, tx, w, p, r, "operation", op, dependencies)
	})
}

type Action struct {
	apicontract.Mutation
	Client string `json:"client"`
	Kind   string `json:"kind"`
	Model  *Model `json:"model,omitempty"`
	Answer string `json:"answer,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (s *Service) owner(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string) (Record, Operation, error) {
	r, err := record(ctx, tx, w, Ref{ID: id, Version: "head"})
	var op Operation
	if err != nil {
		return r, op, err
	}
	if r.Kind != "operation" || decode(r.Value, &op) != nil || op.User != p.UserID || p.OperationID != "" {
		return r, op, access.ErrForbidden
	}
	return r, op, s.checkRefs(ctx, tx, p, w, append(append([]Ref{}, r.Refs...), r.Ref))
}
func (s *Service) currentPolicy(ctx context.Context, tx query, p access.Principal, w string, op Operation) (Policy, error) {
	var version string
	if err := tx.QueryRow(ctx, `SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2`, w, op.Policy.ID).Scan(&version); err != nil {
		return Policy{}, err
	}
	if version != op.Policy.Version {
		return Policy{}, ErrState
	}
	return s.policy(ctx, tx, p, w, op.Policy)
}
func (s *Service) Act(ctx context.Context, p access.Principal, w, id string, req Action) (apicontract.Response[apicontract.Reference], error) {
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		old, op, err := s.owner(ctx, tx, p, w, id)
		if err != nil {
			return Record{}, err
		}
		switch req.Kind {
		case "takeover":
			if req.Client == "" || op.State == "applied" || op.State == "cancelled" {
				return Record{}, ErrState
			}
			op.Client = req.Client
			if req.Model != nil {
				if !validModel(*req.Model) {
					return Record{}, ErrInvalid
				}
				op.Model = *req.Model
			}
			if op.State == "waiting" {
				op.State = "ready"
			}
			if op.State == "sending" {
				op.Reason = "request_result_unknown"
			}
		case "answer":
			if op.Client != req.Client || op.State != "question" || req.Answer == "" {
				return Record{}, ErrState
			}
			event, err := s.writeRelated(ctx, tx, p, w, "client-answer:"+r.Version, "answer", struct {
				Operation Ref    `json:"operation"`
				Text      string `json:"text"`
			}{old.Ref, req.Answer}, append(append([]Ref{}, old.Refs...), old.Ref), id)
			if err != nil {
				return Record{}, err
			}
			op.Turn = &event
			op.State = "ready"
			op.Reason = ""
			var since time.Time
			if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&since); err != nil {
				return Record{}, err
			}
			op.EvidenceSince = &since
		case "resume":
			if req.Client != op.Client || (op.State != "incomplete" && op.State != "failed") {
				return Record{}, ErrState
			}
			var since time.Time
			if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&since); err != nil {
				return Record{}, err
			}
			op.EvidenceSince = &since
			op.State = "ready"
			op.Reason = ""
		case "cancel":
			op.State = "cancelled"
			op.Reason = "cancelled_by_requester"
		case "hold":
			if req.Client != op.Client || op.State != "ready" {
				return Record{}, ErrState
			}
			op.State = "incomplete"
			op.Reason = req.Reason
		case "adopt":
			if op.Client != req.Client || op.Proposal == nil {
				return Record{}, ErrState
			}
			if err = s.adopt(ctx, tx, p, w, old.Ref, &op); err != nil {
				return Record{}, err
			}
		default:
			return Record{}, ErrInvalid
		}
		return s.save(ctx, tx, w, p, r, "operation", op, old.Refs)
	})
}
func (s *Service) writeRelated(ctx context.Context, tx pgx.Tx, p access.Principal, w, id, kind string, value any, refs []Ref, roots ...string) (Ref, error) {
	return accepted(s.mutate(ctx, p, w, id, apicontract.Mutation{OperationID: "record:" + id, ExpectedVersion: "0"}, value, func(tx pgx.Tx, r Ref) (Record, error) { return s.save(ctx, tx, w, p, r, kind, value, refs, roots...) }, tx))
}

type Begin struct {
	apicontract.Mutation
	Client        string `json:"client"`
	InputDigest   string `json:"input_digest"`
	PromptVersion string `json:"prompt_version"`
	Sources       []Ref  `json:"sources"`
}
type Turn struct {
	RequestID     string          `json:"request_id"`
	Operation     Ref             `json:"operation"`
	Client        string          `json:"client"`
	Model         Model           `json:"model"`
	InputDigest   string          `json:"input_digest"`
	PromptVersion string          `json:"prompt_version"`
	Response      json.RawMessage `json:"response,omitempty"`
	State         string          `json:"state"`
	TimeoutMS     int64           `json:"timeout_ms"`
	Status        int             `json:"http_status,omitempty"`
	Failure       string          `json:"failure,omitempty"`
}

func (s *Service) Begin(ctx context.Context, p access.Principal, w, id string, req Begin) (apicontract.Response[apicontract.Reference], error) {
	if len(req.InputDigest) != 64 || req.PromptVersion == "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		old, op, err := s.owner(ctx, tx, p, w, id)
		if err != nil {
			return Record{}, err
		}
		if req.Client == "" || op.Client != req.Client || op.State != "ready" {
			return Record{}, ErrState
		}
		policy, err := s.currentPolicy(ctx, tx, p, w, op)
		if err != nil {
			return Record{}, err
		}
		reader := access.Principal{UserID: p.UserID, OperationID: op.ReadOperation}
		observed, err := s.mcpSources(ctx, tx, w, op.ReadOperation, nil, false)
		if err != nil {
			return Record{}, err
		}
		req.Sources = unique(append(req.Sources, observed...))
		if err = s.checkRefs(ctx, tx, reader, w, req.Sources); err != nil {
			return Record{}, err
		}
		var models int
		var created time.Time
		var deadline *time.Time
		if err = tx.QueryRow(ctx, `SELECT models,created_at,deadline FROM client_operation_heads WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).Scan(&models, &created, &deadline); err != nil {
			return Record{}, err
		}
		if deadline == nil {
			v := time.Now().UTC().Add(time.Duration(policy.Limits.Seconds) * time.Second)
			deadline = &v
			op.StartedAt = new(time.Time)
			*op.StartedAt = time.Now().UTC()
		}
		if models >= policy.Limits.Models || !time.Now().Before(*deadline) {
			op.State = "incomplete"
			op.Reason = "operation_limit"
			return s.save(ctx, tx, w, p, r, "operation", op, old.Refs)
		}
		for _, budget := range budgets(policy, op.Model) {
			if models == 0 {
				n, _ := new(big.Rat).SetString(budget.Amount)
				if n.Sign() == 0 {
					op.State = "incomplete"
					op.Reason = "cost_exhausted"
					return s.save(ctx, tx, w, p, r, "operation", op, old.Refs)
				}
			} else {
				cost, err := usage.ObserveCosts(ctx, tx, p, w, "operation", []string{id}, budget)
				if err != nil {
					return Record{}, err
				}
				op.Cost = &cost
				if cost.State != "observed" {
					op.State = "incomplete"
					op.Reason = "cost_" + cost.State
					return s.save(ctx, tx, w, p, r, "operation", op, old.Refs)
				}
			}
		}
		turn := Turn{Operation: old.Ref, Client: req.Client, Model: op.Model, InputDigest: req.InputDigest, PromptVersion: req.PromptVersion, State: "intent", RequestID: req.OperationID, TimeoutMS: time.Until(*deadline).Milliseconds()}
		pin, err := s.writeRelated(ctx, tx, p, w, "client-turn:"+r.Version, "turn", turn, append(append([]Ref{}, old.Refs...), req.Sources...), id)
		if err != nil {
			return Record{}, err
		}
		op.Turn = &pin
		op.State = "sending"
		intentRecord, err := record(ctx, tx, w, pin)
		if err != nil {
			return Record{}, err
		}
		if err = s.observe(ctx, tx, p, w, old.Ref, op, turn, intentRecord, Reply{}, true, true); err != nil {
			return Record{}, err
		}
		op.Reason = ""
		if _, err = tx.Exec(ctx, `UPDATE client_operation_heads SET models=models+1,deadline=$3 WHERE workspace_id=$1 AND id=$2`, w, id, deadline); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, p, r, "operation", op, unique(append(append([]Ref{}, old.Refs...), req.Sources...)))
	})
}
func (s *Service) adopt(ctx context.Context, tx pgx.Tx, p access.Principal, w string, operation Ref, op *Operation) error {
	policy, err := s.currentPolicy(ctx, tx, p, w, *op)
	if err != nil {
		return err
	}
	r, err := record(ctx, tx, w, *op.Proposal)
	if err != nil {
		return err
	}
	var proposal Proposal
	if decode(r.Value, &proposal) != nil || !complete(proposal) {
		return ErrState
	}
	if err = s.candidateEvidence(ctx, tx, w, *op, policy, proposal); err != nil {
		return err
	}
	input, err := selection(policy.Fixed, proposal)
	if err != nil {
		return err
	}
	resolution, err := s.resolveSelection(ctx, tx, p, w, *op, input)
	if err != nil {
		return err
	}
	if !resolution.Ready {
		return ErrState
	}
	for _, required := range policy.Fixed.Include {
		found := false
		for _, placement := range resolution.Manifest.Placements {
			if placement.Ref == required {
				found = true
			}
		}
		if !found {
			return ErrState
		}
	}
	ref, err := accepted(s.putSelection(ctx, p, p, w, op.Target.ID, SelectionWrite{Name: op.Goal, Mutation: apicontract.Mutation{OperationID: "adopt:" + op.Proposal.ID, ExpectedVersion: op.Target.Version}, Input: input, Items: proposal.Items, Operation: &operation, Proposal: op.Proposal}, tx))
	if err != nil {
		return err
	}
	op.Applied = &ref
	op.State = "applied"
	op.Reason = ""
	return nil
}
