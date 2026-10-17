package publications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Backend interface {
	Connection(string, string) (Connection, bool)
	Create(context.Context, Connection, Draft, string) Outcome
	Observe(context.Context, Connection, string, int64) Outcome
}
type Service struct {
	Store   *persistence.Store
	Access  *access.Service
	Runs    *runs.Service
	Backend Backend
}

func New(store *persistence.Store, a *access.Service, r *runs.Service, b Backend) *Service {
	s := &Service{store, a, r, b}
	previous := r.BeforeStart
	r.BeforeStart = func(ctx context.Context, tx pgx.Tx, w, id string) error {
		if previous != nil {
			if err := previous(ctx, tx, w, id); err != nil {
				return err
			}
		}
		return s.beforeStart(ctx, tx, w, id)
	}
	return s
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if !json.Valid(b) || d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func unique(refs []Ref) []Ref {
	out := []Ref{}
	seen := map[Ref]bool{}
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
func record(ctx context.Context, q query, w string, ref Ref) (Record, error) {
	var r Record
	var refs []byte
	err := q.QueryRow(ctx, `SELECT id,version,owner_id,kind,position::text,payload,refs FROM publication_records WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY publication_records.position DESC LIMIT 1`, w, ref.ID, ref.Version).Scan(&r.ID, &r.Version, &r.Owner, &r.Kind, &r.Position, &r.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &r.Refs)
	}
	return r, err
}
func (s *Service) refs(ctx context.Context, q query, p access.Principal, w string, refs []Ref) error {
	var ok bool
	err := q.QueryRow(ctx, `SELECT `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, raw(unique(refs))).Scan(&ok)
	if err == nil && !ok {
		return access.ErrForbidden
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
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, ref Ref, owner, kind string, v any, refs []Ref) (Record, error) {
	r := Record{Ref: ref, Owner: owner, Kind: kind, Value: raw(v), Refs: unique(refs)}
	err := tx.QueryRow(ctx, `INSERT INTO publication_records SELECT $1,$2,$3,$4,$5,sequence+1,$6,$7 FROM shared_streams WHERE workspace_id=$1 RETURNING position::text`, w, r.ID, r.Version, r.Owner, r.Kind, r.Value, raw(r.Refs)).Scan(&r.Position)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, ref.ID)
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
			if !exists {
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
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "publication-" + kind, Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, err := apply(tx, Ref{ID: ref.ID, Version: ref.Version})
		return raw(r), err
	})
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	value := req.Value
	if req.ValueUTF8 != nil {
		if len(value) > 0 {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		value = json.RawMessage(*req.ValueUTF8)
	}
	var d Draft
	if !strings.HasPrefix(id, "publication-draft:") || id == "publication-draft:" || decode(value, &d) != nil || !validDraft(d) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, id, "draft", "save", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		refs, err := s.Runs.SourcesTx(ctx, tx, p, w, d.Sources)
		if err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, id, "draft", d, refs)
	})
}
func (s *Service) authority(ctx context.Context, q query, w string, r Request, owner string, send bool) error {
	p := access.Principal{UserID: r.Actor.User}
	check := func(target, action string) error {
		if tx, ok := q.(pgx.Tx); ok {
			return s.Access.CheckTx(ctx, tx, p, w, target, action)
		}
		return s.Access.Check(ctx, p, w, target, action)
	}
	if err := check(owner, "get"); err != nil {
		return err
	}
	draft, err := record(ctx, q, w, r.Draft)
	if err != nil {
		return err
	}
	if err = s.refs(ctx, q, p, w, append(draft.Refs, draft.Ref)); err != nil {
		return err
	}
	if !send {
		return nil
	}
	if err = check(owner, "update"); err != nil {
		return err
	}
	if err = check(owner, "execute"); err != nil {
		return err
	}
	current, err := record(ctx, q, w, Ref{ID: r.Draft.ID, Version: "head"})
	if err != nil {
		return err
	}
	if current.Ref != r.Draft {
		return ErrState
	}
	if r.Actor.Control != nil {
		var b []byte
		var version string
		err = q.QueryRow(ctx, `SELECT r.version,r.payload FROM automation_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version WHERE r.workspace_id=$1 AND r.id=$2 AND r.kind='control'`, w, r.Actor.Control.ID).Scan(&version, &b)
		var c automation.Control
		if err != nil || version != r.Actor.Control.Version || json.Unmarshal(b, &c) != nil || c.Kind != "hopper" || !c.Enabled || !c.CanRead || !c.CanCommit || c.Controller != r.Actor.User {
			return access.ErrForbidden
		}
		if err = check("", "manage"); err != nil {
			return err
		}
	}
	if s.Backend == nil {
		return ErrState
	}
	if c, ok := s.Backend.Connection(w, r.Input.Connection); !ok || c != r.Connection {
		return access.ErrForbidden
	}
	return nil
}
func (s *Service) Preview(ctx context.Context, p access.Principal, w, id string, draft Ref) (Request, error) {
	r, err := s.Read(ctx, p, w, draft)
	if err != nil {
		return Request{}, err
	}
	var d Draft
	if !fixed(draft) || r.Kind != "draft" || decode(r.Value, &d) != nil || !strings.HasPrefix(id, "publication:") || id == "publication:" {
		return Request{}, ErrInvalid
	}
	tag := marker(w, id)
	if s.Backend == nil {
		return Request{}, ErrState
	}
	connection, ok := s.Backend.Connection(w, d.Connection)
	if !ok {
		return Request{}, access.ErrForbidden
	}
	return Request{Draft: draft, Input: d, Connection: connection, Actor: Actor{Kind: "human", User: p.UserID}, Marker: tag, Body: d.Body + "\n\n" + tag, Run: "run:" + id, State: "preview", At: time.Now().UTC()}, nil
}
func (s *Service) Submit(ctx context.Context, p access.Principal, w, id string, req Submit) (apicontract.Response[apicontract.Reference], error) {
	return s.submit(ctx, p, w, id, req, nil)
}

// Called by the authenticated server automation owner, not a public actor switch.
func (s *Service) SubmitAutomation(ctx context.Context, w, id string, req Submit, control Ref) (apicontract.Response[apicontract.Reference], error) {
	if !fixed(control) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	r, err := s.Store.Database().Query(ctx, `SELECT payload FROM automation_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='control'`, w, control.ID, control.Version)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	defer r.Close()
	var c automation.Control
	if !r.Next() {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	var b []byte
	if r.Scan(&b) != nil || json.Unmarshal(b, &c) != nil {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	r.Close()
	return s.submit(ctx, access.Principal{UserID: c.Controller}, w, id, req, &control)
}
func (s *Service) submit(ctx context.Context, p access.Principal, w, id string, req Submit, control *Ref) (apicontract.Response[apicontract.Reference], error) {
	if req.ExpectedVersion != "0" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	input, err := s.Preview(ctx, p, w, id, req.Draft)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if control != nil {
		input.Actor.Kind = "automation"
		input.Actor.Control = control
	}
	input.State = "accepted"
	if err = s.authority(ctx, s.Store.Database(), w, input, req.Draft.ID, true); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	return s.mutate(ctx, p, w, id, req.Draft.ID, "request", "execute", req.Mutation, struct {
		Submit  Submit
		Control *Ref
	}{req, control}, func(tx pgx.Tx, ref Ref) (Record, error) {
		if err := s.authority(ctx, tx, w, input, req.Draft.ID, true); err != nil {
			return Record{}, err
		}
		input.Accepted = ref
		draft, err := record(ctx, tx, w, req.Draft)
		if err != nil {
			return Record{}, err
		}
		refs := append(draft.Refs, draft.Ref)
		if control != nil {
			refs = append(refs, *control)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO publication_queue(workspace_id,id,run_id,state) VALUES($1,$2,$3,'accepted')`, w, id, input.Run); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, req.Draft.ID, "request", input, refs)
	})
}
func (s *Service) beforeStart(ctx context.Context, tx pgx.Tx, w, run string) error {
	var q query = s.Store.Database()
	if tx != nil {
		q = tx
	}
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM publication_queue WHERE workspace_id=$1 AND run_id=$2`, w, run).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rec, err := record(ctx, q, w, Ref{ID: id, Version: "head"})
	if err != nil {
		return err
	}
	var r Request
	if decode(rec.Value, &r) != nil || (r.State != "accepted" && r.State != "sending") {
		return ErrState
	}
	if err = s.authority(ctx, q, w, r, rec.Owner, true); err != nil {
		return err
	}
	// Native assignment must not turn a server operation into an invented CLI.
	var assigned bool
	err = q.QueryRow(ctx, `SELECT COALESCE((SELECT payload->'assignment' IS NOT NULL FROM run_records WHERE workspace_id=$1 AND id=$2 AND kind='run' ORDER BY run_records.position DESC LIMIT 1),false)`, w, run).Scan(&assigned)
	if err != nil {
		return err
	}
	if assigned {
		return access.ErrForbidden
	}
	return nil
}
