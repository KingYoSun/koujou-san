package runs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
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
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service) *Service {
	return &Service{store, a, c}
}
func runID(id string) string { return "run:" + strings.TrimPrefix(id, "run:") }
func contextRef(r contexts.Ref) Ref {
	k := r.Kind
	if k == "sentence" || k == "paragraph" || k == "section" || k == "document" {
		k = "context"
	}
	return Ref{ID: k + ":" + r.ID, Version: r.Revision}
}
func manifestRefs(ref contexts.Ref, m contexts.Manifest) []Ref {
	out := []Ref{contextRef(ref)}
	for _, r := range append(append([]contexts.Ref{}, m.Selection.Include...), m.Selection.Exclude...) {
		out = append(out, contextRef(r))
	}
	for _, placement := range m.Placements {
		out = append(out, contextRef(placement.Ref))
	}
	if m.Baseline != nil {
		out = append(out, contextRef(*m.Baseline))
	}
	for _, r := range m.Memberships {
		out = append(out, contextRef(r))
	}
	return out
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
func record(ctx context.Context, q query, w string, r Ref) (Record, error) {
	var out Record
	var refs []byte
	err := q.QueryRow(ctx, `SELECT id,version,run_id,kind,position::text,payload,refs FROM run_records
 WHERE workspace_id=$1 AND id=$2 AND ($3='head' OR version=$3) ORDER BY run_records.position DESC LIMIT 1`, w, r.ID, r.Version).Scan(&out.ID, &out.Version, &out.RunID, &out.Kind, &out.Position, &out.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &out.Refs)
	}
	return out, err
}
func (s *Service) refs(ctx context.Context, q query, p access.Principal, w string, refs []Ref) error {
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
func (s *Service) inputRefs(ctx context.Context, q query, w string, r Run) ([]Ref, error) {
	var refs []byte
	err := q.QueryRow(ctx, `SELECT refs FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='input'`, w, r.Input.ID, r.Input.Version).Scan(&refs)
	var out []Ref
	if err == nil {
		err = json.Unmarshal(refs, &out)
	}
	return out, err
}
func (s *Service) input(ctx context.Context, q query, w string, r Run) (Spec, error) {
	i, err := record(ctx, q, w, r.Input)
	if err != nil {
		return Spec{}, err
	}
	var v Spec
	err = json.Unmarshal(i.Value, &v)
	return v, err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, r Ref) (Record, error) {
	if !fixed(r) && r.Version != "head" {
		return Record{}, ErrInvalid
	}
	v, err := record(ctx, s.Store.Database(), w, r)
	if err != nil {
		return Record{}, access.ErrForbidden
	}
	if err = s.Access.Check(ctx, p, w, v.RunID, "get"); err != nil {
		return Record{}, err
	}
	h, err := record(ctx, s.Store.Database(), w, Ref{ID: v.RunID, Version: "head"})
	if err != nil {
		return Record{}, err
	}
	var head Run
	_ = json.Unmarshal(h.Value, &head)
	deps, err := s.inputRefs(ctx, s.Store.Database(), w, head)
	if err != nil {
		return Record{}, err
	}
	deps = append(deps, v.Refs...)
	deps = append(deps, v.Ref)
	if err = s.refs(ctx, s.Store.Database(), p, w, deps); err != nil {
		return Record{}, err
	}
	if v.Kind == "link" {
		var link Link
		if json.Unmarshal(v.Value, &link) != nil {
			return Record{}, ErrInvalid
		}
		if _, err = s.Read(ctx, p, w, link.Next); err != nil {
			return Record{}, err
		}
	}
	return v, nil
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w, root, id, kind, version string, value any, refs []Ref) error {
	refs = unique(refs)
	_, err := tx.Exec(ctx, `INSERT INTO run_records SELECT $1,$2,$3,$4,$5,sequence+1,$6,$7 FROM shared_streams WHERE workspace_id=$1`, w, root, id, version, kind, raw(value), raw(refs))
	if err != nil {
		return err
	}
	// Child access always also checks the current Run policy. Existing resource
	// grants survive updates, just as they do for Context records.
	_, err = tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, id)
	return err
}
func (s *Service) mutate(ctx context.Context, p access.Principal, w, id, action, kind string, m apicontract.Mutation, payload any, apply func(pgx.Tx, apicontract.Reference) (Run, []Ref, error), guards ...func(query) error) (apicontract.Response[apicontract.Reference], error) {
	if p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	root := runID(id)
	check := func(tx pgx.Tx) error {
		target := root
		if action == "register" {
			target = ""
		}
		var q query = s.Store.Database()
		var err error
		if tx == nil {
			err = s.Access.Check(ctx, p, w, target, action)
		} else {
			q = tx
			err = s.Access.CheckTx(ctx, tx, p, w, target, action)
		}
		if err != nil {
			return err
		}
		for _, guard := range guards {
			if err = guard(q); err != nil {
				return err
			}
		}
		if action == "register" || p.DeviceID != "" {
			return nil
		}
		r, _, err := loadRun(ctx, q, w, root)
		if err != nil {
			return err
		}
		deps, err := s.inputRefs(ctx, q, w, r)
		if err != nil {
			return err
		}
		return s.refs(ctx, q, p, w, deps)
	}
	if err := check(nil); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	actor := "user:" + p.UserID
	if p.DeviceID != "" {
		actor = "device:" + p.DeviceID
	}
	scope := persistence.Scope{ActorID: actor, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := check(tx); err != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: root, Kind: kind, Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, refs, err := apply(tx, ref)
		if err != nil {
			return nil, err
		}
		if err = s.save(ctx, tx, w, root, root, "run", ref.Version, r, refs); err != nil {
			return nil, err
		}
		// Shared synchronization receives a small head, never all output or inputs.
		return raw(Record{Ref: Ref{ID: root, Version: ref.Version}, RunID: root, Kind: "run", Value: raw(r), Refs: unique(refs)}), nil
	})
}
func loadRun(ctx context.Context, q query, w, id string) (Run, Record, error) {
	r, err := record(ctx, q, w, Ref{ID: runID(id), Version: "head"})
	var v Run
	if err == nil {
		err = json.Unmarshal(r.Value, &v)
	}
	return v, r, err
}
func (s *Service) sources(ctx context.Context, tx pgx.Tx, p access.Principal, w string, refs []Ref) ([]Ref, error) {
	out := []Ref{}
	for _, r := range refs {
		if !fixed(r) {
			return nil, ErrInvalid
		}
		if err := s.Access.CheckTx(ctx, tx, p, w, r.ID, "get"); err != nil {
			return nil, err
		}
		if err := s.Access.CheckVersion(ctx, tx, p, w, r.ID, r.Version); err != nil {
			return nil, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2 AND version=$3
 UNION ALL SELECT 1 FROM shared_changes WHERE workspace_id=$1 AND resource_id=$2 AND version=$3
 UNION ALL SELECT 1 FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3
 UNION ALL SELECT 1 FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3)`, w, r.ID, r.Version).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrInvalid
		}
		out = append(out, r)
		if rr, err := record(ctx, tx, w, r); err == nil {
			out = append(out, rr.Refs...)
			head, _, err := loadRun(ctx, tx, w, rr.RunID)
			if err != nil {
				return nil, err
			}
			deps, err := s.inputRefs(ctx, tx, w, head)
			if err != nil {
				return nil, err
			}
			out = append(out, deps...)
			if rr.Kind == "link" {
				var link Link
				if json.Unmarshal(rr.Value, &link) != nil {
					return nil, ErrInvalid
				}
				child, _, err := loadRun(ctx, tx, w, link.Next.ID)
				if err != nil {
					return nil, err
				}
				deps, err := s.inputRefs(ctx, tx, w, child)
				if err != nil {
					return nil, err
				}
				out = append(out, deps...)
			}

		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		} else if strings.HasPrefix(r.ID, "manifest:") {
			rec, err := s.Contexts.ReadTx(ctx, tx, p, w, contexts.Ref{Kind: "manifest", ID: strings.TrimPrefix(r.ID, "manifest:"), Revision: r.Version})
			if err != nil {
				return nil, err
			}
			var manifest contexts.Manifest
			if json.Unmarshal(rec.Value, &manifest) != nil {
				return nil, ErrInvalid
			}
			out = append(out, manifestRefs(rec.Ref, manifest)...)
		}
	}
	if err := s.refs(ctx, tx, p, w, out); err != nil {
		return nil, err
	}
	return unique(out), nil
}
func artifactsValid(items []contexts.Fixed) bool {
	for _, item := range items {
		if contexts.ValidateFixed(item) != nil {
			return false
		}
	}
	return true
}
func (s *Service) Create(ctx context.Context, p access.Principal, w, id string, req Create) (apicontract.Response[apicontract.Reference], error) {
	if id == "" || strings.TrimPrefix(id, "run:") == "" || req.ExpectedVersion != "0" || validate(req.Spec) != nil || !artifactsValid(req.Spec.Artifacts) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "register", "run-create", req.Mutation, req, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		spec := req.Spec
		refs, err := s.sources(ctx, tx, p, w, spec.Sources)
		if err != nil {
			return Run{}, nil, err
		}
		if spec.Previous != nil {
			prev, err := record(ctx, tx, w, *spec.Previous)
			if err != nil || prev.Kind != "run" {
				return Run{}, nil, ErrInvalid
			}
			deps, err := s.sources(ctx, tx, p, w, []Ref{*spec.Previous})
			if err != nil {
				return Run{}, nil, err
			}
			refs = append(refs, deps...)
		}
		if spec.Manifest != nil {
			rec, err := s.Contexts.ReadTx(ctx, tx, p, w, *spec.Manifest)
			if err != nil {
				return Run{}, nil, err
			}
			var manifest contexts.Manifest
			if json.Unmarshal(rec.Value, &manifest) != nil || runID(manifest.RunID) != ref.ID {
				return Run{}, nil, ErrInvalid
			}
			refs = append(refs, manifestRefs(rec.Ref, manifest)...)
			if manifest.State == "ready" {
				if manifest.InputUTF8 == nil || !json.Valid([]byte(*manifest.InputUTF8)) {
					return Run{}, nil, ErrInvalid
				}
				spec.Input.Context = raw(contextRef(rec.Ref))
			} else if manifest.State != "none" {
				return Run{}, nil, ErrInvalid
			}
		}
		validation := spec
		validation.Manifest = nil
		if validate(validation) != nil {
			return Run{}, nil, ErrInvalid
		}
		input := Ref{ID: "run-input:" + strings.TrimPrefix(ref.ID, "run:"), Version: ref.Version}
		refs = append(refs, Ref{ID: ref.ID, Version: ref.Version})
		if err = s.save(ctx, tx, w, ref.ID, input.ID, "input", input.Version, spec, refs); err != nil {
			return Run{}, nil, err
		}
		if spec.Previous != nil {
			link := Link{Previous: *spec.Previous, Next: Ref{ID: ref.ID, Version: ref.Version}}
			if err = s.save(ctx, tx, w, spec.Previous.ID, "run-link:"+strings.TrimPrefix(ref.ID, "run:"), "link", ref.Version, link, []Ref{link.Previous, link.Next}); err != nil {
				return Run{}, nil, err
			}
		}
		r := Run{Name: spec.Name, Input: input, Previous: spec.Previous, RequestedBy: p.UserID, CreatedAt: time.Now().UTC(), State: "queued", Observation: "known", ReportedThrough: "0"}
		return r, []Ref{input}, nil
	})
}
func (s *Service) Action(ctx context.Context, p access.Principal, w, id, action string, m apicontract.Mutation) (apicontract.Response[apicontract.Reference], error) {
	if action != "request-start" && action != "cancel" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "execute", "run-"+action, m, struct {
		Action   string
		Mutation apicontract.Mutation
	}{action, m}, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return r, nil, err
		}
		deps, err := s.inputRefs(ctx, tx, w, r)
		if err != nil {
			return r, nil, err
		}
		if err = s.refs(ctx, tx, p, w, deps); err != nil {
			return r, nil, err
		}
		if terminal(r.State) {
			return r, nil, ErrState
		}
		if action == "request-start" {
			if r.ExecutionRequestedAt != nil || r.State != "queued" {
				return r, nil, ErrState
			}
			now := time.Now().UTC()
			r.ExecutionRequestedAt = &now
		} else {
			r.CancelRequested = true
		}
		return r, old.Refs, nil
	})
}
func (s *Service) Answer(ctx context.Context, p access.Principal, w, id string, req Answer) (apicontract.Response[apicontract.Reference], error) {
	if req.PauseID == "" || len(req.Value) == 0 {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "update", "run-answer", req.Mutation, req, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return r, nil, err
		}
		deps, err := s.inputRefs(ctx, tx, w, r)
		if err != nil {
			return r, nil, err
		}
		if err = s.refs(ctx, tx, p, w, deps); err != nil {
			return r, nil, err
		}
		added, err := s.sources(ctx, tx, p, w, req.Sources)
		if err != nil {
			return r, nil, err
		}
		if err = s.receive(ctx, tx, w, ref, &r, req.PauseID, req.Value, ResponseOrigin{Kind: "user", ID: p.UserID}, added); err != nil {
			return r, nil, err
		}
		return r, old.Refs, nil
	})
}

func (s *Service) receive(ctx context.Context, tx pgx.Tx, w string, ref apicontract.Reference, r *Run, pauseID string, value json.RawMessage, origin ResponseOrigin, added []Ref) error {
	if r.State != "paused" || r.ActivePause == nil || len(value) == 0 {
		return ErrState
	}
	pr, err := record(ctx, tx, w, *r.ActivePause)
	if err != nil {
		return err
	}
	var pause Pause
	if json.Unmarshal(pr.Value, &pause) != nil {
		return ErrInvalid
	}
	kind := pause.ResponseKind
	if kind == "" {
		kind = "user"
	}
	if pause.ID != pauseID || pause.State != "waiting" || kind != origin.Kind {
		return ErrState
	}
	if origin.Kind == "user" {
		if err = s.refs(ctx, tx, access.Principal{UserID: origin.ID}, w, pr.Refs); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	pause.State = "received"
	pause.Response = value
	pause.ResponseID = rand.Text()
	pause.AnsweredBy = origin.ID
	pause.AnsweredAt = &now
	pause.ResponseOrigin = &origin
	next := Ref{ID: pr.ID, Version: ref.Version}
	r.ActivePause = &next
	return s.save(ctx, tx, w, ref.ID, next.ID, "pause", next.Version, pause, append(pr.Refs, added...))
}

// Report accepts only an assigned, current device principal. The Runner that
// obtains the assignment is implemented separately; a user cannot forge events.
func (s *Service) Report(ctx context.Context, p access.Principal, w, id string, e Event) (apicontract.Response[apicontract.Reference], error) {
	if p.DeviceID == "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	return s.mutate(ctx, p, w, id, "report", "run-event", e.Mutation, e, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return r, nil, err
		}
		if terminal(r.State) || r.ExecutionRequestedAt == nil {
			return r, nil, ErrState
		}
		sequence, err := strconv.ParseInt(e.Sequence, 10, 64)
		previous, _ := strconv.ParseInt(r.ReportedThrough, 10, 64)
		if err != nil || sequence != previous+1 || strconv.FormatInt(sequence, 10) != e.Sequence {
			return r, nil, ErrInvalid
		}
		if !artifactsValid(e.Artifacts) {
			return r, nil, ErrInvalid
		}
		// Source refs are explicit metadata; the registered owner must currently be
		// able to read newly attached sources. Existing raw output is never fetched.
		var owner string
		if err = tx.QueryRow(ctx, `SELECT registered_by FROM access_devices WHERE workspace_id=$1 AND id=$2`, w, p.DeviceID).Scan(&owner); err != nil {
			return r, nil, err
		}
		deps, err := s.sources(ctx, tx, access.Principal{UserID: owner}, w, e.Sources)
		if err != nil {
			return r, nil, err
		}
		refs := append([]Ref{{ID: ref.ID, Version: ref.Version}}, deps...)
		now := time.Now().UTC()
		switch e.Type {
		case "preparation-blocked":
			if r.State != "queued" || r.StartedAt != nil || r.Assignment == nil {
				return r, nil, ErrState
			}
			if e.Source != "local_configuration_missing" && e.Source != "input_binding_unsupported" && e.Source != "process_start_failed" {
				return r, nil, ErrInvalid
			}
			r.Preparation = e.Source
		case "prepared":
			if r.Assignment == nil || r.State != "queued" || r.Assignment.PreparedAt != nil || r.CancelRequested {
				return r, nil, ErrState
			}
			if (r.Assignment.Area == nil) != (e.PreparedArea == nil) || (r.Assignment.Area != nil && *r.Assignment.Area != *e.PreparedArea) {
				return r, nil, ErrInvalid
			}
			r.Assignment.PreparedAt = &now
			r.Preparation = ""
		case "renew":
			if r.Assignment == nil {
				return r, nil, ErrState
			}
			r.Assignment.ExpiresAt = now.Add(assignmentLifetime)
		case "started":
			if r.State != "queued" || r.CancelRequested || (r.Assignment != nil && r.Assignment.PreparedAt == nil) {
				return r, nil, ErrState
			}
			r.State = "running"
			r.StartedAt = &now
			r.Execution = e.Execution
		case "output":
			if r.State != "running" && r.State != "paused" {
				return r, nil, ErrState
			}
			if e.Channel == "" || e.Source == "" || (len(e.Value) == 0 && len(e.Artifacts) == 0) {
				return r, nil, ErrInvalid
			}
			out := Output{Sequence: e.Sequence, Channel: e.Channel, Source: e.Source, Value: e.Value, Artifacts: e.Artifacts, At: now}
			if err = s.save(ctx, tx, w, ref.ID, "run-output:"+strings.TrimPrefix(ref.ID, "run:")+":"+e.Sequence, "output", ref.Version, out, refs); err != nil {
				return r, nil, err
			}
		case "pause":
			if r.State != "running" || r.ActivePause != nil || e.Pause == nil || e.Pause.ID == "" || e.Pause.Reason == "" || len(e.Pause.Request) == 0 {
				return r, nil, ErrState
			}
			if e.Pause.ResponseKind != "" && e.Pause.ResponseKind != "user" && e.Pause.ResponseKind != "event" {
				return r, nil, ErrInvalid
			}
			pid := "run-pause:" + strings.TrimPrefix(ref.ID, "run:") + ":" + e.Pause.ID
			if _, err = record(ctx, tx, w, Ref{ID: pid, Version: "head"}); !errors.Is(err, pgx.ErrNoRows) {
				return r, nil, ErrState
			}
			pause := Pause{PauseRequest: *e.Pause, State: "waiting", CreatedAt: now}
			if err = s.save(ctx, tx, w, ref.ID, pid, "pause", ref.Version, pause, refs); err != nil {
				return r, nil, err
			}
			r.ActivePause = &Ref{ID: pid, Version: ref.Version}
			r.State = "paused"
		case "observed-response":
			if e.Source == "" {
				return r, nil, ErrInvalid
			}
			if err = s.receive(ctx, tx, w, ref, &r, e.PauseID, e.Value, ResponseOrigin{Kind: "event", ID: "device:" + p.DeviceID, Source: e.Source}, deps); err != nil {
				return r, nil, err
			}
		case "delivered", "resumed":
			if r.State != "paused" || r.ActivePause == nil {
				return r, nil, ErrState
			}
			pr, err := record(ctx, tx, w, *r.ActivePause)
			if err != nil {
				return r, nil, err
			}
			var pause Pause
			_ = json.Unmarshal(pr.Value, &pause)
			if pause.ID != e.PauseID || pause.ResponseID == "" || pause.ResponseID != e.ResponseID {
				return r, nil, ErrState
			}
			if e.Type == "delivered" {
				if pause.State != "received" {
					return r, nil, ErrState
				}
				pause.State = "delivered"
				pause.DeliveredAt = &now
			} else {
				if pause.State != "delivered" {
					return r, nil, ErrState
				}
				pause.State = "resolved"
				pause.ResumedAt = &now
				r.State = "running"
			}
			next := Ref{ID: pr.ID, Version: ref.Version}
			if err = s.save(ctx, tx, w, ref.ID, next.ID, "pause", next.Version, pause, pr.Refs); err != nil {
				return r, nil, err
			}
			if e.Type == "resumed" {
				r.ActivePause = nil
			} else {
				r.ActivePause = &next
			}
		case "finished", "cancelled":
			if e.Type == "finished" {
				if (r.State != "running" && r.State != "paused") || e.ExitCode == nil {
					return r, nil, ErrState
				}
				r.ExitCode = e.ExitCode
				r.State = "succeeded"
				if *e.ExitCode != 0 {
					r.State = "failed"
				}
				spec, err := s.input(ctx, tx, w, r)
				if err != nil {
					return r, nil, err
				}
				r.Assessment = assess(spec.Derivation, e.Report)
			} else {
				r.State = "cancelled"
			}
			r.FinishedAt = &now
			if r.ActivePause != nil {
				pr, err := record(ctx, tx, w, *r.ActivePause)
				if err != nil {
					return r, nil, err
				}
				var pause Pause
				_ = json.Unmarshal(pr.Value, &pause)
				pause.State = "cancelled"
				if err = s.save(ctx, tx, w, ref.ID, pr.ID, "pause", ref.Version, pause, pr.Refs); err != nil {
					return r, nil, err
				}
				r.ActivePause = nil
			}
			// This authoritative result and its source event share the same tx. No
			// Hopper definition exists until #25, so no publication is fabricated.
		case "unknown":
			r.Observation = "unknown"
		case "known":
			r.Observation = "known"
		default:
			return r, nil, ErrInvalid
		}
		if e.Type != "unknown" && e.Type != "renew" {
			r.Observation = "known"
		}
		r.ReportedThrough = e.Sequence
		if terminal(r.State) {
			if err = s.save(ctx, tx, w, ref.ID, "run-result:"+strings.TrimPrefix(ref.ID, "run:"), "result", ref.Version, r, refs); err != nil {
				return r, nil, err
			}
		}
		accepted := struct {
			Event Event     `json:"event"`
			Actor string    `json:"actor"`
			At    time.Time `json:"at"`
		}{e, "device:" + p.DeviceID, now}
		if err = s.save(ctx, tx, w, ref.ID, fmt.Sprintf("run-event:%s:%s", strings.TrimPrefix(ref.ID, "run:"), e.Sequence), "event", ref.Version, accepted, refs); err != nil {
			return r, nil, err
		}
		return r, old.Refs, nil
	}, func(q query) error {
		r, _, err := loadRun(ctx, q, w, id)
		if err != nil {
			return err
		}
		return s.checkAssignment(ctx, q, p, w, r, e.Generation, e.Type == "renew" || terminal(r.State))
	})
}
