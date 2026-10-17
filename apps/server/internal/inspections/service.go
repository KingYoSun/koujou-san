package inspections

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Store     *persistence.Store
	Access    *access.Service
	Contexts  *contexts.Service
	Runs      *runs.Service
	Workflows *workflows.Service
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service, r *runs.Service, w *workflows.Service) *Service {
	s := &Service{store, a, c, r, w}
	previous := r.BeforeStart
	r.BeforeStart = func(ctx context.Context, tx pgx.Tx, workspace, run string) error {
		if previous != nil {
			if err := previous(ctx, tx, workspace, run); err != nil {
				return err
			}
		}
		return s.beforeStart(ctx, tx, workspace, run)
	}
	r.AssessResult = s.assessResult
	return s
}
func unique(refs []Ref) []Ref {
	out := []Ref{}
	seen := map[Ref]bool{}
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
func record(ctx context.Context, q query, w string, ref Ref) (Record, error) {
	var r Record
	var refs []byte
	err := q.QueryRow(ctx, `SELECT id,version,owner_id,kind,position::text,payload,refs FROM inspection_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY inspection_records.position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&r.ID, &r.Version, &r.Owner, &r.Kind, &r.Position, &r.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &r.Refs)
	}
	return r, err
}
func (s *Service) refs(ctx context.Context, q query, p access.Principal, w string, refs []Ref) error {
	var allowed bool
	err := q.QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(unique(refs))).Scan(&allowed)
	if err == nil && !allowed {
		err = access.ErrForbidden
	}
	return err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	if !fixed(ref) && ref.Version != "head" {
		return Record{}, ErrInvalid
	}
	r, err := record(ctx, s.Store.Database(), w, ref)
	if err != nil {
		return Record{}, access.ErrForbidden
	}
	if err = s.Access.Check(ctx, p, w, r.Owner, "get"); err != nil {
		return Record{}, err
	}
	if err = s.refs(ctx, s.Store.Database(), p, w, append(r.Refs, r.Ref)); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, ref Ref, owner, kind string, value any, refs []Ref) (Record, error) {
	r := Record{Ref: ref, Owner: owner, Kind: kind, Value: raw(value), Refs: unique(refs)}
	err := tx.QueryRow(ctx, `INSERT INTO inspection_records SELECT $1,$2,$3,$4,$5,sequence+1,$6,$7 FROM shared_streams WHERE workspace_id=$1 RETURNING position::text`, w, r.ID, r.Version, r.Owner, r.Kind, r.Value, raw(r.Refs)).Scan(&r.Position)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, r.ID)
	}
	return r, err
}
func (s *Service) mutate(ctx context.Context, p access.Principal, w, id, owner, kind, permission string, m apicontract.Mutation, payload any, apply func(pgx.Tx, Ref) (Record, error)) (apicontract.Response[apicontract.Reference], error) {
	if p.OperationID != "" || p.DeviceID != "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	check := func(tx pgx.Tx) error {
		var q query = s.Store.Database()
		if tx != nil {
			q = tx
		}
		target, action := owner, permission
		if permission == "save" {
			var exists bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2)`, w, id).Scan(&exists); err != nil {
				return err
			}
			action = "update"
			if !exists && owner == id {
				target = ""
				action = "register"
			}
		}
		if tx != nil {
			return s.Access.CheckTx(ctx, tx, p, w, target, action)
		}
		return s.Access.Check(ctx, p, w, target, action)
	}
	if err := check(nil); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if check(tx) != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "inspection-" + kind, Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, err := apply(tx, Ref{ID: ref.ID, Version: ref.Version})
		return raw(r), err
	})
}
func (s *Service) definition(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Definition, Record, error) {
	r, err := record(ctx, q, w, ref)
	if err != nil {
		return Definition{}, r, access.ErrForbidden
	}
	if r.Kind != "definition" {
		return Definition{}, r, ErrInvalid
	}
	if err = s.refs(ctx, q, p, w, append(r.Refs, r.Ref)); err != nil {
		return Definition{}, r, err
	}
	var d Definition
	err = decode(r.Value, &d)
	return d, r, err
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	value := req.Value
	if req.ValueUTF8 != nil {
		if len(value) > 0 {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		value = json.RawMessage(*req.ValueUTF8)
	}
	kind, _, _ := strings.Cut(id, ":")
	if id == kind+":" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	if kind != "inspection" && kind != "inspection-evidence" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, id, kind, "save", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		refs := []Ref{}
		if kind == "inspection" {
			var d Definition
			if decode(value, &d) != nil || validate(d) != nil {
				return Record{}, ErrInvalid
			}
			template, err := s.Workflows.Read(ctx, p, w, d.Template)
			if err != nil {
				return Record{}, err
			}
			if template.Kind != "run-definition" {
				return Record{}, ErrInvalid
			}
			refs = append(append(refs, template.Refs...), template.Ref)
			for _, observations := range d.Observations {
				for _, evidence := range observations {
					r, err := record(ctx, tx, w, evidence)
					if err != nil || r.Kind != "evidence" {
						return Record{}, ErrInvalid
					}
					refs = append(append(refs, r.Refs...), r.Ref)
				}
			}
			for _, c := range d.Criteria {
				if c.Source != nil {
					if c.Source.Kind != "sentence" && c.Source.Kind != "paragraph" && c.Source.Kind != "section" && c.Source.Kind != "document" {
						return Record{}, ErrInvalid
					}
					source, err := s.Contexts.ReadTx(ctx, tx, p, w, *c.Source)
					if err != nil {
						return Record{}, err
					}
					refs = append(refs, Ref{ID: "context:" + source.Ref.ID, Version: source.Ref.Revision})
				}
			}
			for _, context := range append(append([]contexts.Ref{}, d.Selection.Include...), d.Selection.Exclude...) {
				r, err := s.Contexts.ReadTx(ctx, tx, p, w, context)
				if err != nil {
					return Record{}, err
				}
				refs = append(refs, Ref{ID: "context:" + r.Ref.ID, Version: r.Ref.Revision})
			}
			if d.Reuse != nil {
				previous, err := s.Runs.Read(ctx, p, w, d.Reuse.Run)
				if err != nil {
					return Record{}, err
				}
				if previous.Kind != "run" {
					return Record{}, ErrInvalid
				}
				refs = append(append(refs, previous.Refs...), previous.Ref)
				for _, e := range d.Reuse.Evidence {
					r, err := record(ctx, tx, w, e)
					if err != nil || r.Kind != "evidence" {
						return Record{}, ErrInvalid
					}
					refs = append(append(refs, r.Refs...), r.Ref)
				}
			}
			if err := s.refs(ctx, tx, p, w, refs); err != nil {
				return Record{}, err
			}
			return s.save(ctx, tx, w, ref, id, "definition", d, refs)
		}
		var e Evidence
		if decode(value, &e) != nil || contexts.ValidateFixed(e.Artifact) != nil || e.CheckedAt.IsZero() || (e.Kind != "machine-observation" && e.Kind != "human-confirmation" && e.Kind != "ai-comment" && e.Kind != "implementer-claim") || (e.State != "available" && e.State != "unobserved" && e.State != "expired" && e.State != "unavailable") {
			return Record{}, ErrInvalid
		}
		for _, target := range e.Targets {
			if contexts.ValidateFixed(target) != nil {
				return Record{}, ErrInvalid
			}
		}
		return s.save(ctx, tx, w, ref, id, "evidence", e, refs)
	})
}

type Adoption struct {
	Definition Ref `json:"definition"`
}
type Adopt struct {
	apicontract.Mutation
	Definition Ref `json:"definition"`
}

func (s *Service) Adopt(ctx context.Context, p access.Principal, w string, req Adopt) (apicontract.Response[apicontract.Reference], error) {
	if !fixed(req.Definition) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	id := "inspection-adoption:" + req.Definition.ID
	return s.mutate(ctx, p, w, id, req.Definition.ID, "adopt", "update", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		d, r, err := s.definition(ctx, tx, p, w, req.Definition)
		if err != nil {
			return Record{}, err
		}
		v, err := s.Workflows.Validate(ctx, p, w, d.Template)
		if err != nil {
			return Record{}, err
		}
		if !v.Valid {
			return Record{}, ErrState
		}
		return s.save(ctx, tx, w, ref, req.Definition.ID, "adoption", Adoption{req.Definition}, append(r.Refs, r.Ref))
	})
}
