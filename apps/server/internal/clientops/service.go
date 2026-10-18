package clientops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Runs        *runs.Service
	Definitions *workflows.Service
	Store       *persistence.Store
	Access      *access.Service
	Contexts    *contexts.Service
	Usage       *usage.Service
}

func New(s *persistence.Store, a *access.Service, c *contexts.Service, u *usage.Service) *Service {
	service := &Service{Store: s, Access: a, Contexts: c, Usage: u, Runs: runs.New(s, a, c)}
	previous := s.Accepted
	s.Accepted = func(ctx context.Context, tx pgx.Tx, command persistence.Command, ref apicontract.Reference, value json.RawMessage) error {
		if previous != nil {
			if err := previous(ctx, tx, command, ref, value); err != nil {
				return err
			}
		}
		return service.observeGenerationSource(ctx, tx, command, ref, value)
	}
	return service
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func pinned(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "0" && r.Version != "head"
}
func resource(r contexts.Ref) string { return "context:" + r.ID }
func refs(v contexts.Selection) []Ref {
	out := []Ref{}
	for _, r := range append(append([]contexts.Ref{}, v.Include...), v.Exclude...) {
		out = append(out, Ref{ID: resource(r), Version: r.Revision})
	}
	return out
}
func record(ctx context.Context, q query, w string, r Ref) (Record, error) {
	var v Record
	var b []byte
	err := q.QueryRow(ctx, `SELECT id,version,kind,root_id,position,payload,refs FROM client_operation_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY position DESC LIMIT 1`, w, r.ID, r.Version).Scan(&v.ID, &v.Version, &v.Kind, &v.Root, &v.Position, &v.Value, &b)
	if err == nil {
		err = json.Unmarshal(b, &v.Refs)
	}
	return v, err
}
func (s *Service) checkRefs(ctx context.Context, q query, p access.Principal, w string, rs []Ref) error {
	if rs == nil {
		rs = []Ref{}
	}
	var allowed bool
	if err := q.QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(rs)).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return access.ErrForbidden
	}
	return nil
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, r Ref) (Record, error) {
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return Record{}, err
	}
	if !pinned(r) && r.Version != "head" {
		return Record{}, ErrInvalid
	}
	v, err := record(ctx, s.Store.Database(), w, r)
	if err != nil {
		return Record{}, access.ErrForbidden
	}
	return v, s.checkRefs(ctx, s.Store.Database(), p, w, append(append([]Ref{}, v.Refs...), v.Ref))
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, p access.Principal, r Ref, kind string, value any, rs []Ref, roots ...string) (Record, error) {
	if rs == nil {
		rs = []Ref{}
	}
	root := ""
	if kind == "operation" {
		root = r.ID
	}
	if len(roots) > 0 {
		root = roots[0]
	}
	v := Record{Ref: r, Kind: kind, Root: root, Value: raw(value), Refs: rs}
	err := tx.QueryRow(ctx, `INSERT INTO client_operation_records SELECT $1,$2,$3,$4,$5,sequence+1,$6,$7 FROM shared_streams WHERE workspace_id=$1 RETURNING position`, w, r.ID, r.Version, kind, root, v.Value, raw(rs)).Scan(&v.Position)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, r.ID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO client_operation_heads(workspace_id,id,kind,state,owner_user,client_id,read_operation) VALUES($1,$2,$3,COALESCE($4::jsonb->>'state',$3),COALESCE($4::jsonb->>'user',$5),COALESCE($4::jsonb->>'client',''),COALESCE($4::jsonb->>'read_operation','')) ON CONFLICT(workspace_id,id) DO UPDATE SET kind=excluded.kind,state=excluded.state,owner_user=excluded.owner_user,client_id=excluded.client_id,read_operation=excluded.read_operation`, w, r.ID, kind, v.Value, p.UserID)
	}
	return v, err
}
func (s *Service) mutate(ctx context.Context, p access.Principal, w, id string, m apicontract.Mutation, payload any, apply func(pgx.Tx, Ref) (Record, error), within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	if p.DeviceID != "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	guard := func(tx pgx.Tx) *apicontract.Problem {
		target, action := "", "register"
		if m.ExpectedVersion != "0" {
			target, action = id, "update"
		}
		if p.OperationID != "" {
			action = "selection_write"
		}
		var err error
		if tx == nil {
			err = s.Access.Check(ctx, p, w, target, action)
		} else {
			err = s.Access.CheckTx(ctx, tx, p, w, target, action)
		}
		if err != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}
	if len(within) == 0 && guard(nil) != nil {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	return s.Store.Apply(ctx, persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: guard}, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "client-operation", Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, r apicontract.Reference) (json.RawMessage, error) {
		v, err := apply(tx, Ref{ID: r.ID, Version: r.Version})
		return raw(v), err
	}, within...)
}

type Write struct {
	apicontract.Mutation
	Value json.RawMessage `json:"value"`
}

func (s *Service) PutPolicy(ctx context.Context, p access.Principal, w, id string, req Write, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	var v Policy
	if decode(req.Value, &v) != nil || !validPolicyKind(id, v) || (v.Mode != "manual" && v.Mode != "proposal" && v.Mode != "auto") || (v.Fixed.Mode != "none" && v.Fixed.Mode != "select") || v.Limits.Models < 1 || v.Limits.MCP < 1 || v.Limits.Seconds < 1 {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	if v.Limits.Cost != nil && !usage.ValidCostBudget(*v.Limits.Cost) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		if !s.Contexts.InTransaction(tx).Preview(ctx, p, w, contexts.ResolveRequest{Selection: v.Fixed}).Ready {
			return Record{}, ErrInvalid
		}
		rs := refs(v.Fixed)
		if v.Generation != nil {
			_, deps, err := s.generationSource(ctx, p, w, v.Generation)
			if err != nil {
				return Record{}, err
			}
			rs = append(rs, deps...)
			rs = append(rs, generationRefs(v.Generation)...)
		}
		if v.Definition != nil {
			rs = append(rs, v.Definition.Sources...)
			if err := s.checkRefs(ctx, tx, p, w, v.Definition.Sources); err != nil {
				return Record{}, err
			}
		}
		for _, scope := range v.Scopes {
			if err := s.Access.CheckTx(ctx, tx, p, w, "scope:"+scope, "get"); err != nil {
				return Record{}, err
			}
			rs = append(rs, Ref{ID: "scope:" + scope})
		}
		return s.save(ctx, tx, w, p, r, "policy", v, rs)
	}, within...)
}

type SelectionWrite struct {
	Name string `json:"name"`
	apicontract.Mutation
	Input     contexts.Selection `json:"input"`
	Items     []Item             `json:"items"`
	Operation *Ref               `json:"operation,omitempty"`
	Proposal  *Ref               `json:"proposal,omitempty"`
}

func (s *Service) PutSelection(ctx context.Context, p access.Principal, w, id string, req SelectionWrite, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	return s.putSelection(ctx, p, p, w, id, req, within...)
}
func (s *Service) putSelection(ctx context.Context, p, reader access.Principal, w, id string, req SelectionWrite, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	if !strings.HasPrefix(id, "client-selection:") {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	if req.Input.Mode != "none" && (req.Input.Mode != "select" || (len(req.Input.Include) == 0 && len(req.Input.Direct) == 0 && len(req.Input.External) == 0)) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		c := s.Contexts.InTransaction(tx)
		originRefs := []Ref{}
		for _, origin := range []*Ref{req.Operation, req.Proposal} {
			if origin != nil {
				if !pinned(*origin) {
					return Record{}, ErrInvalid
				}
				v, err := record(ctx, tx, w, *origin)
				if err != nil {
					return Record{}, err
				}
				if err = s.checkRefs(ctx, tx, p, w, append(append([]Ref{}, v.Refs...), v.Ref)); err != nil {
					return Record{}, err
				}
				originRefs = append(originRefs, v.Refs...)
			}
		}
		resolved := c.Preview(ctx, reader, w, contexts.ResolveRequest{Selection: req.Input})
		if !resolved.Ready {
			return Record{}, ErrInvalid
		}
		manifestID := "client-selection-" + r.Version
		// The owning selection service authorizes the write. Resolution above still
		// uses the MCP operation's current read/classification boundary.
		writer := access.Principal{UserID: p.UserID}
		out, err := c.PutSelectionManifest(ctx, writer, w, manifestID, contexts.Write{Mutation: apicontract.Mutation{OperationID: "client-selection:" + r.Version, ExpectedVersion: "0"}, Value: raw(contexts.ResolveRequest{Selection: req.Input})})
		if err != nil {
			return Record{}, err
		}
		if out.Error != nil || out.Data == nil {
			return Record{}, ErrState
		}
		manifest := contexts.Ref{Kind: "manifest", ID: manifestID, Revision: out.Data.Version}
		rs := append(refs(req.Input), originRefs...)
		for _, placement := range resolved.Manifest.Placements {
			rs = append(rs, Ref{ID: resource(placement.Ref), Version: placement.Ref.Revision})
		}
		rs = append(rs, Ref{ID: "manifest:" + manifestID, Version: manifest.Revision})
		if req.Operation != nil {
			rs = append(rs, *req.Operation)
		}
		if req.Proposal != nil {
			rs = append(rs, *req.Proposal)
		}
		return s.save(ctx, tx, w, p, r, "selection", Selection{Name: req.Name, Input: req.Input, Manifest: manifest, Operation: req.Operation, Proposal: req.Proposal, Items: req.Items}, unique(rs))
	}, within...)
}

type Request struct {
	Metadata bool   `json:"metadata,omitempty"`
	Kind     string `json:"kind"`
	History  bool   `json:"history"`
	ID       string `json:"id,omitempty"`
	Root     string `json:"root,omitempty"`
	After    int64  `json:"after,omitempty"`
	AfterID  string `json:"after_id,omitempty"`
	Limit    int    `json:"limit"`
}
type Page struct {
	Items   []Record `json:"items"`
	After   int64    `json:"after"`
	AfterID string   `json:"after_id"`
}

func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Request) (Page, error) {
	page := Page{Items: []Record{}}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return page, err
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 100 {
		return page, ErrInvalid
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT r.id,r.version,r.kind,r.root_id,r.position,CASE WHEN $11 THEN jsonb_build_object('name',r.payload->>'name','goal',r.payload->>'goal','state',r.payload->>'state') ELSE r.payload END,r.refs FROM client_operation_records r LEFT JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND ($4='' OR r.kind=$4) AND ($5='' OR r.id=$5) AND ($6 OR h.resource_id IS NOT NULL) AND (r.position>$7 OR (r.position=$7 AND r.id>$8 AND $8<>'')) AND ($10='' OR r.root_id=$10) AND `+access.ReadFilterSQL("r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))")+` ORDER BY r.position,r.id LIMIT $9`, w, p.UserID, p.OperationID, req.Kind, req.ID, req.History, req.After, req.AfterID, req.Limit, req.Root, req.Metadata)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Record
		var b []byte
		if err = rows.Scan(&r.ID, &r.Version, &r.Kind, &r.Root, &r.Position, &r.Value, &b); err != nil {
			return page, err
		}
		if err = json.Unmarshal(b, &r.Refs); err != nil {
			return page, err
		}
		if req.Metadata {
			r.MetadataOnly = true
			r.Dependencies = r.Refs
			r.Refs = []Ref{}
		}
		page.Items = append(page.Items, r)
		page.After, page.AfterID = r.Position, r.ID
	}
	return page, rows.Err()
}
