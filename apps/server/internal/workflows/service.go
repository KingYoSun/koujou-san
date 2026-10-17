package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Store    *persistence.Store
	Access   *access.Service
	Contexts *contexts.Service
	Runs     *runs.Service
	Observer interface {
		Current(context.Context, access.Principal, string, string) (githubobserve.Snapshot, error)
	}
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service, r *runs.Service) *Service {
	s := &Service{Store: store, Access: a, Contexts: c, Runs: r}
	r.BeforeStart = s.beforeStart
	return s
}
func kind(id string) string { k, _, _ := strings.Cut(id, ":"); return k }
func decode(raw []byte, v any) error {
	if !json.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
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
	err := q.QueryRow(ctx, `SELECT id,owner_id,version,kind,position::text,payload,refs FROM workflow_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY workflow_records.position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&r.ID, &r.Owner, &r.Version, &r.Kind, &r.Position, &r.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &r.Refs)
	}
	return r, err
}
func (s *Service) checkRefs(ctx context.Context, q query, p access.Principal, w string, refs []Ref) error {
	var allowed bool
	err := q.QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(unique(refs))).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return access.ErrForbidden
	}
	return nil
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	if !fixed(ref) && ref.Version != "head" {
		return Record{}, ErrInvalid
	}
	r, err := record(ctx, s.Store.Database(), w, ref)
	if err != nil {
		return Record{}, access.ErrForbidden
	}
	if err = s.Access.Check(ctx, p, w, r.ID, "get"); err != nil {
		return Record{}, err
	}
	if r.Owner != r.ID {
		if err = s.Access.Check(ctx, p, w, r.Owner, "get"); err != nil {
			return Record{}, err
		}
		owner, err := record(ctx, s.Store.Database(), w, Ref{ID: r.Owner, Version: "head"})
		if err != nil {
			return Record{}, err
		}
		if err = s.checkRefs(ctx, s.Store.Database(), p, w, owner.Refs); err != nil {
			return Record{}, err
		}
	}
	if err = s.checkRefs(ctx, s.Store.Database(), p, w, append(r.Refs, r.Ref)); err != nil {
		return Record{}, err
	}
	if r.Kind == "run-definition" {
		var d Definition
		if decode(r.Value, &d) != nil {
			return Record{}, ErrInvalid
		}
		if err = s.externalAccess(ctx, s.Store.Database(), p, w, d); err != nil {
			return Record{}, err
		}
	}
	return r, nil
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, ref Ref, k string, value any, refs []Ref, owners ...string) (Record, error) {
	owner := ref.ID
	if len(owners) > 0 {
		owner = owners[0]
	}
	r := Record{Ref: ref, Owner: owner, Kind: k, Value: raw(value), Refs: unique(refs)}
	err := tx.QueryRow(ctx, `INSERT INTO workflow_records SELECT $1,$2,$3,$4,$5,sequence+1,$6,$7 FROM shared_streams WHERE workspace_id=$1 RETURNING position::text`, w, ref.ID, owner, ref.Version, k, r.Value, raw(r.Refs)).Scan(&r.Position)
	if err != nil {
		return r, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, ref.ID)
	return r, err
}
func (s *Service) mutate(ctx context.Context, p access.Principal, w, id, action, k string, m apicontract.Mutation, payload any, apply func(pgx.Tx, Ref) (Record, error)) (apicontract.Response[apicontract.Reference], error) {
	if p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	check := func(tx pgx.Tx) error {
		var q query = s.Store.Database()
		if tx != nil {
			q = tx
		}
		target := id
		permission := action
		if action == "save" {
			var exists bool
			if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2)`, w, id).Scan(&exists); err != nil {
				return err
			}
			permission = "update"
			if !exists {
				permission = "register"
				target = ""
			}
		}
		if action == "start" {
			if req, ok := payload.(Start); ok {
				target = req.Adoption.ID
			}
			permission = "execute"
		}
		if tx != nil {
			return s.Access.CheckTx(ctx, tx, p, w, target, permission)
		}
		return s.Access.Check(ctx, p, w, target, permission)
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
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "workflow-" + k, Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, err := apply(tx, Ref{ID: ref.ID, Version: ref.Version})
		return raw(r), err
	})
}
func (s *Service) definition(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Definition, Record, error) {
	r, err := record(ctx, q, w, ref)
	if err != nil {
		return Definition{}, r, access.ErrForbidden
	}
	if r.Kind != "run-definition" && r.Kind != "workflow-definition" {
		return Definition{}, r, ErrInvalid
	}
	if err = s.checkRefs(ctx, q, p, w, append(r.Refs, r.Ref)); err != nil {
		return Definition{}, r, err
	}
	var d Definition
	err = decode(r.Value, &d)
	if err == nil {
		err = s.externalAccess(ctx, q, p, w, d)
	}
	return d, r, err
}
func (s *Service) externalAccess(ctx context.Context, q query, p access.Principal, w string, d Definition) error {
	targets := []string{}
	if d.CI != nil {
		targets = append(targets, d.CI.Target)
	}
	if d.Write != nil {
		targets = append(targets, d.Write.Target)
	}
	for _, target := range targets {
		var err error
		if tx, ok := q.(pgx.Tx); ok {
			err = s.Access.CheckTx(ctx, tx, p, w, "github:"+target, "get")
		} else {
			err = s.Access.Check(ctx, p, w, "github:"+target, "get")
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) definitionRefs(ctx context.Context, tx pgx.Tx, p access.Principal, w string, d Definition) ([]Ref, error) {
	if err := s.externalAccess(ctx, tx, p, w, d); err != nil {
		return nil, err
	}
	refs := []Ref{}
	if d.Spec != nil {
		refs = append(refs, d.Spec.Sources...)
		if d.Spec.Manifest != nil {
			refs = append(refs, Ref{ID: "manifest:" + d.Spec.Manifest.ID, Version: d.Spec.Manifest.Revision})
		}
	}
	for _, r := range []*contexts.Ref{d.Baseline} {
		if r != nil {
			refs = append(refs, Ref{ID: r.Kind + ":" + r.ID, Version: r.Revision})
		}
	}
	if d.Selection != nil {
		for _, r := range append(append([]contexts.Ref{}, d.Selection.Include...), d.Selection.Exclude...) {
			refs = append(refs, Ref{ID: "context:" + r.ID, Version: r.Revision})
		}
	}
	for _, n := range d.Nodes {
		if n.Definition != nil {
			if !fixed(*n.Definition) {
				return nil, ErrInvalid
			}
			_, r, err := s.definition(ctx, tx, p, w, *n.Definition)
			if err != nil {
				return nil, err
			}
			refs = append(refs, r.Ref)
			refs = append(refs, r.Refs...)
		}
	}
	if err := s.checkRefs(ctx, tx, p, w, refs); err != nil {
		return nil, err
	}
	return unique(refs), nil
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	k := kind(id)
	var d Definition
	value := req.Value
	if req.ValueUTF8 != nil {
		if len(value) > 0 {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		value = json.RawMessage(*req.ValueUTF8)
	}
	if (k != "run-definition" && k != "workflow-definition") || id == k+":" || decode(value, &d) != nil || d.Schema != k+"-v1" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "save", k, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		refs, err := s.definitionRefs(ctx, tx, p, w, d)
		if err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, k, d, refs)
	})
}
func (s *Service) validation(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Validation, error) {
	d, r, err := s.definition(ctx, q, p, w, ref)
	if err != nil {
		return Validation{}, err
	}
	checked := append([]Ref{r.Ref}, r.Refs...)
	var denied error
	problems := validate(d, func(ref Ref) (Definition, error) {
		v, child, err := s.definition(ctx, q, p, w, ref)
		if err != nil {
			denied = err
		}
		checked = append(checked, child.Ref)
		return v, err
	})
	if denied != nil {
		return Validation{}, denied
	}
	capProblems, capRefs, err := s.capabilityProblems(ctx, q, p, w, d)
	if err != nil {
		return Validation{}, err
	}
	problems = append(problems, capProblems...)
	checked = append(checked, capRefs...)
	for i, node := range d.Nodes {
		if node.Definition == nil {
			continue
		}
		child, _, err := s.definition(ctx, q, p, w, *node.Definition)
		if err != nil {
			return Validation{}, err
		}
		ps, rs, err := s.capabilityProblems(ctx, q, p, w, child)
		if err != nil {
			return Validation{}, err
		}
		problems = append(problems, prefixProblems(fmt.Sprintf("/nodes/%d/definition", i), ps)...)
		checked = append(checked, rs...)
	}
	checked = unique(checked)
	var missing string
	err = q.QueryRow(ctx, `SELECT ref.id FROM jsonb_to_recordset($2::jsonb) ref(id text,version text) WHERE NOT EXISTS(
 SELECT 1 FROM workflow_records WHERE workspace_id=$1 AND id=ref.id AND version=ref.version
 UNION ALL SELECT 1 FROM context_records WHERE workspace_id=$1 AND resource_id=ref.id AND revision=ref.version
 UNION ALL SELECT 1 FROM run_records WHERE workspace_id=$1 AND id=ref.id AND version=ref.version
 UNION ALL SELECT 1 FROM inspection_records WHERE workspace_id=$1 AND id=ref.id AND version=ref.version
 UNION ALL SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=ref.id AND version=ref.version
 UNION ALL SELECT 1 FROM shared_changes WHERE workspace_id=$1 AND resource_id=ref.id AND version=ref.version
 UNION ALL SELECT 1 FROM run_runners WHERE workspace_id=$1 AND device_id=substring(ref.id from 8) AND ref.id LIKE 'runner:%' AND payload->>'version'=ref.version
 ) LIMIT 1`, w, raw(checked)).Scan(&missing)
	if err == nil {
		problems = append(problems, Problem{"/references/" + strings.ReplaceAll(strings.ReplaceAll(missing, "~", "~0"), "/", "~1"), "reference_unavailable"})
	} else if !notFound(err) {
		return Validation{}, err
	}
	return Validation{Definition: r.Ref, Valid: len(problems) == 0, Problems: problems, Checked: checked}, nil
}
func (s *Service) Validate(ctx context.Context, p access.Principal, w string, ref Ref) (Validation, error) {
	return s.validation(ctx, s.Store.Database(), p, w, ref)
}

type Verify struct {
	apicontract.Mutation
	Definition Ref `json:"definition"`
}

func (s *Service) Verify(ctx context.Context, p access.Principal, w string, req Verify) (apicontract.Response[apicontract.Reference], error) {
	if !fixed(req.Definition) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, "validation:"+req.Definition.ID, "save", "validation", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		v, err := s.validation(ctx, tx, p, w, req.Definition)
		if err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, "validation", v, v.Checked)
	})
}

type Adopt struct {
	apicontract.Mutation
	Definition Ref `json:"definition"`
	Validation Ref `json:"validation"`
}

func (s *Service) Adopt(ctx context.Context, p access.Principal, w string, req Adopt) (apicontract.Response[apicontract.Reference], error) {
	if !fixed(req.Definition) || !fixed(req.Validation) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, "adoption:"+req.Definition.ID, "save", "adoption", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		old, err := record(ctx, tx, w, req.Validation)
		if err != nil {
			return Record{}, err
		}
		var previous Validation
		if old.Kind != "validation" || json.Unmarshal(old.Value, &previous) != nil || !previous.Valid || previous.Definition != req.Definition {
			return Record{}, ErrState
		}
		v, err := s.validation(ctx, tx, p, w, req.Definition)
		if err != nil {
			return Record{}, err
		}
		if !v.Valid || !bytes.Equal(raw(previous.Checked), raw(v.Checked)) {
			return Record{}, ErrState
		}
		if err = s.checkRefs(ctx, tx, p, w, append(v.Checked, req.Validation)); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, "adoption", Adoption{req.Definition, req.Validation}, append(v.Checked, req.Validation))
	})
}
func notFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
