package hopper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
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
	Store        *persistence.Store
	Access       *access.Service
	Automation   *automation.Service
	Runs         *runs.Service
	Workflows    *workflows.Service
	Publications *publications.Service
	Graph        *graphindex.Service
}

func New(store *persistence.Store, a *access.Service, auto *automation.Service, r *runs.Service, w *workflows.Service, p *publications.Service, g *graphindex.Service) *Service {
	s := &Service{store, a, auto, r, w, p, g}
	previous := store.Accepted
	store.Accepted = func(ctx context.Context, tx pgx.Tx, c persistence.Command, ref apicontract.Reference, state json.RawMessage) error {
		if previous != nil {
			if err := previous(ctx, tx, c, ref, state); err != nil {
				return err
			}
		}
		return s.accepted(ctx, tx, c, ref, state)
	}
	return s
}
func hash(v any) string { h := sha256.Sum256(raw(v)); return hex.EncodeToString(h[:]) }
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if !json.Valid(b) || d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func record(ctx context.Context, q query, w string, ref Ref) (Record, error) {
	var r Record
	var refs []byte
	err := q.QueryRow(ctx, `SELECT id,version,kind,position,payload,refs FROM hopper_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&r.ID, &r.Version, &r.Kind, &r.Position, &r.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &r.Refs)
	}
	return r, err
}
func (s *Service) refs(ctx context.Context, q query, p access.Principal, w string, refs []Ref) error {
	if refs == nil {
		refs = []Ref{}
	}
	var allowed bool
	err := q.QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(refs)).Scan(&allowed)
	if err == nil && !allowed {
		return access.ErrForbidden
	}
	return err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return Record{}, err
	}
	if !fixed(ref) && ref.Version != "head" {
		return Record{}, ErrInvalid
	}
	r, err := record(ctx, s.Store.Database(), w, ref)
	if err != nil {
		return Record{}, access.ErrForbidden
	}
	if err = s.refs(ctx, s.Store.Database(), p, w, append(append([]Ref{}, r.Refs...), r.Ref)); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, ref Ref, kind string, value any, refs []Ref) (Record, error) {
	if refs == nil {
		refs = []Ref{}
	}
	r := Record{Ref: ref, Kind: kind, Value: raw(value), Refs: refs}
	err := tx.QueryRow(ctx, `INSERT INTO hopper_records SELECT $1,$2,$3,$4,sequence+1,$5,$6 FROM shared_streams WHERE workspace_id=$1 RETURNING position`, w, ref.ID, ref.Version, kind, r.Value, raw(refs)).Scan(&r.Position)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, ref.ID)
	}
	return r, err
}
func problem(err error) *apicontract.Problem {
	if err == nil {
		return nil
	}
	return &apicontract.Problem{Code: "forbidden"}
}
func (s *Service) mutation(ctx context.Context, p access.Principal, w, id, action string, m apicontract.Mutation, value any, apply func(pgx.Tx, Ref) (Record, error)) (apicontract.Response[apicontract.Reference], error) {
	if p.DeviceID != "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	check := func(tx pgx.Tx) error {
		target, permission := id, action
		if action == "save" {
			target = ""
			permission = "register"
			if m.ExpectedVersion != "0" {
				target = id
				permission = "update"
			}
		}
		if action == "manage" {
			target = ""
		}
		if p.OperationID != "" {
			permission = "hopper_write"
			if action == "manage" {
				permission = "hopper_manage"
			}
		}
		if tx == nil {
			return s.Access.Check(ctx, p, w, target, permission)
		}
		return s.Access.CheckTx(ctx, tx, p, w, target, permission)
	}
	if err := check(nil); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	return s.Store.Apply(ctx, persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem { return problem(check(tx)) }}, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "hopper-" + action, Mutation: m, Payload: raw(value)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, err := apply(tx, Ref{ID: ref.ID, Version: ref.Version})
		return raw(r), err
	})
}
func validDefinition(d Definition) bool {
	b := d.Bounds
	if b.Cost != nil && !usage.ValidCostBudget(*b.Cost) {
		return false
	}
	if d.Name == "" || len(d.Targets) == 0 || !validCondition(d.Condition) || (d.Mode != "auto" && d.Mode != "proposal") || !fixed(d.Action.Template) || (d.Action.Kind != "run" && d.Action.Kind != "issue") || b.Depth < 1 || b.Publications < 1 || b.Evaluations < 1 || b.Seconds < 1 || b.PerEvaluation < 1 {
		return false
	}
	for _, t := range d.Targets {
		if (t.Kind != "run" && t.Kind != "github") || t.ID == "" {
			return false
		}
	}
	if p := d.Problem; p != nil {
		if p.Repository == "" || p.Target == "" || p.Rule == "" || len(p.IdentityPath) == 0 || p.WindowSeconds < 1 || (p.Mode != "link_open" && p.Mode != "hold" && p.Mode != "recur") {
			return false
		}
	}
	for _, m := range d.Action.Mappings {
		if len(m.Path) == 0 || (d.Action.Kind == "run" && m.To != "prompt" && m.To != "command" && m.To != "context" && m.To != "repository.commit") || (d.Action.Kind == "issue" && m.To != "title" && m.To != "body") {
			return false
		}
	}
	repositoryMapped := false
	for _, m := range d.Action.Mappings {
		repositoryMapped = repositoryMapped || m.To == "repository.commit"
	}
	if repositoryMapped {
		if d.Action.Repository == nil || d.Action.Repository.ID == "" {
			return false
		}
	} else if !runs.ValidRepository(d.Action.Repository) {
		return false
	}
	return true
}
func (s *Service) template(ctx context.Context, p access.Principal, w string, d Definition) ([]Ref, error) {
	refs := []Ref{d.Action.Template}
	if d.Action.Kind == "run" {
		r, err := s.Workflows.Read(ctx, p, w, d.Action.Template)
		if err != nil {
			return nil, err
		}
		var def workflows.Definition
		if decode(r.Value, &def) != nil || def.Schema != "run-definition-v1" || def.Spec == nil {
			return nil, ErrInvalid
		}
		v, err := s.Workflows.Validate(ctx, p, w, d.Action.Template)
		if err != nil || !v.Valid {
			return nil, ErrInvalid
		}
		refs = append(refs, r.Refs...)
	} else {
		r, err := s.Publications.Read(ctx, p, w, d.Action.Template)
		if err != nil || r.Kind != "draft" {
			return nil, ErrInvalid
		}
		refs = append(refs, r.Refs...)
	}
	return refs, nil
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	action := "save"
	if strings.HasPrefix(id, "hopper-active:") {
		action = "manage"
	}
	return s.mutation(ctx, p, w, id, action, req.Mutation, req.Value, func(tx pgx.Tx, ref Ref) (Record, error) {
		if strings.HasPrefix(id, "hopper:") && id != "hopper:" {
			var d Definition
			if decode(req.Value, &d) != nil || !validDefinition(d) {
				return Record{}, ErrInvalid
			}
			refs, err := s.template(ctx, p, w, d)
			if err != nil {
				return Record{}, err
			}
			for _, target := range d.Targets {
				if target.ID != "*" {
					id := target.ID
					if target.Kind == "github" {
						id = "github:" + id
					}
					refs = append(refs, Ref{ID: id})
				}
			}
			if err = s.refs(ctx, tx, p, w, refs); err != nil {
				return Record{}, err
			}
			return s.save(ctx, tx, w, ref, "definition", d, refs)
		}
		var a Activation
		if decode(req.Value, &a) != nil || !fixed(a.Definition) || id != "hopper-active:"+strings.TrimPrefix(a.Definition.ID, "hopper:") {
			return Record{}, ErrInvalid
		}
		r, err := record(ctx, tx, w, a.Definition)
		if err != nil || r.Kind != "definition" {
			return Record{}, ErrInvalid
		}
		refs := append(append([]Ref{}, r.Refs...), r.Ref)
		if err = s.refs(ctx, tx, p, w, refs); err != nil {
			return Record{}, err
		}
		var d Definition
		_ = json.Unmarshal(r.Value, &d)
		a.Controller = p.UserID
		if _, err = tx.Exec(ctx, `DELETE FROM hopper_targets WHERE workspace_id=$1 AND definition_id=$2`, w, a.Definition.ID); err != nil {
			return Record{}, err
		}
		if a.Enabled {
			for _, t := range d.Targets {
				if _, err = tx.Exec(ctx, `INSERT INTO hopper_targets VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, w, t.Kind, t.ID, a.Definition.ID); err != nil {
					return Record{}, err
				}
			}
		}
		return s.save(ctx, tx, w, ref, "activation", a, refs)
	})
}
func (s *Service) Query(ctx context.Context, p access.Principal, w string, req Request) (Page, error) {
	out := Page{Items: []Record{}, After: req.After, AfterID: req.AfterID}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return out, err
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.Limit < 1 || req.Limit > 100 || req.After < 0 || (req.Mode != "heads" && req.Mode != "history") {
		return out, ErrInvalid
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT r.id,r.version,r.kind,r.position,r.payload,r.refs FROM hopper_records r WHERE r.workspace_id=$1 AND (r.position>$4 OR ($10<>'' AND r.position=$4 AND r.id>$10)) AND ($5='' OR r.kind=$5) AND ($6='' OR r.id=$6) AND ($7='' OR r.payload->>'root'=$7) AND ($8='history' OR EXISTS(SELECT 1 FROM shared_versions h WHERE h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version AND NOT h.deleted)) AND `+access.ReadFilterSQL(`r.refs||jsonb_build_array(jsonb_build_object('id',r.id,'version',r.version))`)+` ORDER BY r.position,r.id LIMIT $9`, w, p.UserID, p.OperationID, req.After, req.Kind, req.ID, req.Root, req.Mode, req.Limit, req.AfterID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Record
		var refs []byte
		if err = rows.Scan(&r.ID, &r.Version, &r.Kind, &r.Position, &r.Value, &refs); err != nil {
			return out, err
		}
		if err = json.Unmarshal(refs, &r.Refs); err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
		out.After = r.Position
		out.AfterID = r.ID
	}
	return out, rows.Err()
}
func mutation(id string) apicontract.Mutation {
	return apicontract.Mutation{OperationID: id, ExpectedVersion: "0"}
}
func structMutation(id, version string) apicontract.Mutation {
	return apicontract.Mutation{OperationID: id, ExpectedVersion: version}
}
func accepted(v apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if v.Data == nil {
		if v.Error != nil && v.Error.Code == "forbidden" {
			return Ref{}, access.ErrForbidden
		}
		if v.Error != nil && v.Error.Code == "invalid_request" {
			return Ref{}, ErrInvalid
		}
		return Ref{}, ErrState
	}
	return Ref{ID: v.Data.ID, Version: v.Data.Version}, nil
}
func missing(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
