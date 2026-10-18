package runs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

// These are logical references. Executable paths and credentials stay on the device.
type Connection struct {
	Kind         string   `json:"kind"`
	Target       string   `json:"target"`
	Version      string   `json:"version"`
	Environment  string   `json:"environment"`
	Subject      string   `json:"subject"`
	Confirmed    bool     `json:"confirmed"`
	Bindings     []string `json:"bindings"`
	Capabilities []string `json:"capabilities"`
	Users        []string `json:"users"`
	Repositories []string `json:"repositories"`
}
type Runner struct {
	Ref
	DeviceID    string       `json:"device_id"`
	RunnerID    string       `json:"runner_id"`
	Connections []Connection `json:"connections"`
	SeenAt      time.Time    `json:"seen_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
}
type RunnerContact struct {
	apicontract.Mutation
	RunnerID    string       `json:"runner_id"`
	Connections []Connection `json:"connections"`
}
type Repository struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
}
type Area struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}
type Assignment struct {
	SessionUnconfirmed bool        `json:"session_unconfirmed,omitempty"`
	Generation         string      `json:"generation"`
	DeviceID           string      `json:"device_id"`
	RunnerID           string      `json:"runner_id"`
	RunnerVersion      string      `json:"runner_version"`
	RequestedBy        string      `json:"requested_by"`
	Connection         Connection  `json:"connection"`
	Required           []string    `json:"required"`
	Input              Ref         `json:"input"`
	Repository         *Repository `json:"repository,omitempty"`
	Area               *Area       `json:"area,omitempty"`
	PreparedAt         *time.Time  `json:"prepared_at,omitempty"`
	ExpiresAt          time.Time   `json:"expires_at"`
}
type Assign struct {
	apicontract.Mutation
	DeviceID       string      `json:"device_id"`
	Repository     *Repository `json:"repository,omitempty"`
	Required       []string    `json:"required"`
	HandoffSource  *Ref        `json:"handoff_source,omitempty"`
	AllowDuplicate bool        `json:"allow_duplicate,omitempty"`
}
type AssignedInput struct {
	Run             Ref              `json:"run"`
	Assignment      Assignment       `json:"assignment"`
	Spec            Spec             `json:"spec"`
	Manifest        *contexts.Record `json:"manifest,omitempty"`
	ReportedThrough string           `json:"reported_through"`
}

const assignmentLifetime = 2 * time.Minute

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

func runner(ctx context.Context, q query, w, device string) (Runner, error) {
	var r Runner
	var payload []byte
	err := q.QueryRow(ctx, `SELECT payload FROM run_runners WHERE workspace_id=$1 AND device_id=$2`, w, device).Scan(&payload)
	if err == nil {
		err = json.Unmarshal(payload, &r)
	}
	return r, err
}
func (s *Service) ContactRunner(ctx context.Context, p access.Principal, w string, req RunnerContact) (apicontract.Response[apicontract.Reference], error) {
	if p.DeviceID == "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	if strings.TrimSpace(req.RunnerID) == "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range req.Connections {
		key := c.Kind + "\x00" + c.Target
		if c.Kind == "" || c.Target == "" || c.Version == "" || c.Environment == "" || c.Subject == "" || seen[key] {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		seen[key] = true
	}
	check := func(q query) error {
		if tx, ok := q.(pgx.Tx); ok {
			return s.Access.CheckTx(ctx, tx, p, w, "", "contact")
		}
		return s.Access.Check(ctx, p, w, "", "contact")
	}
	if err := check(s.Store.Database()); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "device:" + p.DeviceID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if check(tx) != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: "runner:" + p.DeviceID, Kind: "runner-contact", Mutation: req.Mutation, Payload: raw(req)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES($1,$2,false) ON CONFLICT DO NOTHING`, w, ref.ID); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		r := Runner{Ref: Ref{ID: ref.ID, Version: ref.Version}, DeviceID: p.DeviceID, RunnerID: req.RunnerID, Connections: req.Connections, SeenAt: now, ExpiresAt: now.Add(assignmentLifetime)}
		_, err := tx.Exec(ctx, `INSERT INTO run_runners VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,device_id) DO UPDATE SET version=EXCLUDED.version,payload=EXCLUDED.payload`, w, p.DeviceID, ref.Version, raw(r))
		return raw(r), err
	})
}
func connection(r Runner, cmd Command) (Connection, string) {
	if time.Now().After(r.ExpiresAt) {
		return Connection{}, "device_absent"
	}
	for _, c := range r.Connections {
		if c.Kind == cmd.Kind && c.Target == cmd.Target {
			if !c.Confirmed {
				return c, "capability_unconfirmed"
			}
			return c, ""
		}
	}
	return Connection{}, "connection_missing"
}
func (s *Service) eligible(ctx context.Context, q query, w, user, device string, spec Spec, repo *Repository, required []string) (Runner, Connection, string, error) {
	var enabled, input, report bool
	var owner string
	err := q.QueryRow(ctx, `SELECT d.enabled,d.can_input,d.can_report,d.registered_by FROM access_devices d JOIN access_members m ON m.workspace_id=d.workspace_id AND m.user_id=d.registered_by AND m.active WHERE d.workspace_id=$1 AND d.id=$2`, w, device).Scan(&enabled, &input, &report, &owner)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (!enabled || !input || !report) {
		return Runner{}, Connection{}, "", access.ErrForbidden
	}
	if err != nil {
		return Runner{}, Connection{}, "", err
	}
	r, err := runner(ctx, q, w, device)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, Connection{}, "device_absent", nil
	}
	if err != nil {
		return r, Connection{}, "", err
	}
	c, reason := connection(r, spec.Input.Command)
	if c.Target != "" && (!slices.Contains(c.Users, user) || repo != nil && !slices.Contains(c.Repositories, repo.ID)) {
		return r, c, "", access.ErrForbidden
	}
	if reason != "" {
		return r, c, reason, nil
	}
	if reason = CapabilityGap(c, spec.Input.Command, required); reason != "" {
		return r, c, reason, nil
	}
	return r, c, "", nil
}

// CapabilityGap is shared by definition validation and the final assignment.
func CapabilityGap(c Connection, command Command, required []string) string {
	for _, b := range command.Binding {
		if !slices.Contains(c.Bindings, b) {
			return "binding_missing"
		}
	}
	for _, need := range required {
		if !slices.Contains(c.Capabilities, need) {
			return "capability_missing"
		}
	}
	return ""
}
func (s *Service) Assign(ctx context.Context, p access.Principal, w, id string, req Assign) (apicontract.Response[apicontract.Reference], error) {
	if req.DeviceID == "" || !ValidRepository(req.Repository) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "execute", "run-assign", req.Mutation, req, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		if s.BeforeStart != nil {
			if err := s.BeforeStart(ctx, tx, w, ref.ID); err != nil {
				return Run{}, nil, err
			}
		}
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return r, nil, err
		}
		if r.State != "queued" || r.CancelRequested || r.Assignment != nil {
			return r, nil, ErrState
		}
		if s.AssignmentGate != nil {
			reason, err := s.AssignmentGate(ctx, tx, w, id)
			if err != nil {
				return r, nil, err
			}
			if reason != "" {
				r.Preparation = reason
				return r, old.Refs, nil
			}
		}
		spec, err := s.input(ctx, tx, w, r)
		if err != nil {
			return r, nil, err
		}
		runner, c, reason, err := s.eligible(ctx, tx, w, p.UserID, req.DeviceID, spec, req.Repository, req.Required)
		if err != nil {
			return r, nil, err
		}
		r.Preparation = reason
		if reason != "" {
			return r, old.Refs, nil
		}
		// The device owner must also be allowed to receive this exact fixed input.
		if err = s.Access.CheckTx(ctx, tx, access.Principal{UserID: deviceOwner(ctx, tx, w, req.DeviceID)}, w, ref.ID, "get"); err != nil {
			return r, nil, err
		}
		deps, err := s.inputRefs(ctx, tx, w, r)
		if err != nil {
			return r, nil, err
		}
		if err = s.refs(ctx, tx, access.Principal{UserID: deviceOwner(ctx, tx, w, req.DeviceID)}, w, deps); err != nil {
			return r, nil, err
		}
		now := time.Now().UTC()
		if r.Handoff != nil {
			h := r.Handoff
			source, head, err := loadRun(ctx, tx, w, h.Source.ID)
			if err != nil {
				return r, nil, err
			}
			if req.HandoffSource == nil || *req.HandoffSource != head.Ref {
				return r, nil, ErrState
			}
			if _, err = s.SourcesTx(ctx, tx, p, w, []Ref{head.Ref}); err != nil {
				return r, nil, err
			}
			judgment := "confirmed_end"
			if source.Observation == "unknown" {
				if !req.AllowDuplicate {
					return r, nil, ErrState
				}
				judgment = "duplicate_possible"
			} else if !terminal(source.State) {
				return r, nil, ErrState
			}
			h.Decision = &HandoffDecision{Source: head.Ref, State: source.State, Observation: source.Observation, Judgment: judgment, By: p.UserID, At: now}
			old.Refs = append(old.Refs, head.Ref)
		}
		a := &Assignment{Generation: rand.Text(), DeviceID: req.DeviceID, RunnerID: runner.RunnerID, RunnerVersion: runner.Version, RequestedBy: p.UserID, Connection: c, Required: req.Required, Input: r.Input, Repository: req.Repository, ExpiresAt: now.Add(assignmentLifetime)}
		if req.Repository != nil {
			digest := sha256.Sum256([]byte(w + "\x00" + ref.ID))
			key := hex.EncodeToString(digest[:16])
			a.Area = &Area{ID: key, Branch: "koujou/run-" + key, Commit: req.Repository.Commit}
		}
		r.Assignment = a
		if r.ExecutionRequestedAt == nil {
			r.ExecutionRequestedAt = &now
		}
		_, err = tx.Exec(ctx, `UPDATE access_resources SET assigned_device=$3 WHERE workspace_id=$1 AND id=$2`, w, ref.ID, req.DeviceID)
		return r, old.Refs, err
	})
}
func deviceOwner(ctx context.Context, q query, w, device string) string {
	var owner string
	_ = q.QueryRow(ctx, `SELECT registered_by FROM access_devices WHERE workspace_id=$1 AND id=$2`, w, device).Scan(&owner)
	return owner
}
func (s *Service) checkAssignment(ctx context.Context, q query, p access.Principal, w string, r Run, generation string, renew bool) error {
	a := r.Assignment
	if a == nil {
		return nil
	} // Only pre-assignment fixtures can report through the internal assigned_device field.
	if a.DeviceID != p.DeviceID || a.Generation != generation || (!renew && time.Now().After(a.ExpiresAt)) {
		return access.ErrForbidden
	}
	user := access.Principal{UserID: a.RequestedBy}
	var err error
	if tx, ok := q.(pgx.Tx); ok {
		err = s.Access.CheckTx(ctx, tx, user, w, runIDFromInput(a.Input.ID), "execute")
	} else {
		err = s.Access.Check(ctx, user, w, runIDFromInput(a.Input.ID), "execute")
	}
	if err != nil {
		return err
	}
	deps, err := s.inputRefs(ctx, q, w, r)
	if err != nil {
		return err
	}
	if err = s.refs(ctx, q, user, w, deps); err != nil {
		return err
	}
	if err = s.refs(ctx, q, access.Principal{UserID: deviceOwner(ctx, q, w, p.DeviceID)}, w, deps); err != nil {
		return err
	}
	spec, err := s.input(ctx, q, w, r)
	if err != nil {
		return err
	}
	rr, c, reason, err := s.eligible(ctx, q, w, a.RequestedBy, p.DeviceID, spec, a.Repository, a.Required)
	if err != nil {
		return err
	}
	if reason != "" || rr.RunnerID != a.RunnerID || c.Version != a.Connection.Version || c.Environment != a.Connection.Environment || c.Subject != a.Connection.Subject {
		return access.ErrForbidden
	}
	return nil
}
func runIDFromInput(id string) string { return "run:" + strings.TrimPrefix(id, "run-input:") }
func (s *Service) Assigned(ctx context.Context, p access.Principal, w, id, generation string) (AssignedInput, error) {
	if p.DeviceID == "" {
		return AssignedInput{}, access.ErrForbidden
	}
	if err := s.Access.Check(ctx, p, w, runID(id), "input"); err != nil {
		return AssignedInput{}, err
	}
	r, rec, err := loadRun(ctx, s.Store.Database(), w, id)
	if err != nil {
		return AssignedInput{}, err
	}
	if r.Assignment == nil {
		return AssignedInput{}, access.ErrForbidden
	}
	if err = s.checkAssignment(ctx, s.Store.Database(), p, w, r, generation, false); err != nil {
		return AssignedInput{}, err
	}
	if s.BeforeStart != nil {
		if err = s.BeforeStart(ctx, nil, w, rec.ID); err != nil {
			return AssignedInput{}, err
		}
	}
	spec, err := s.input(ctx, s.Store.Database(), w, r)
	if err != nil {
		return AssignedInput{}, err
	}
	result := AssignedInput{Run: rec.Ref, Assignment: *r.Assignment, Spec: spec, ReportedThrough: r.ReportedThrough}
	if spec.Manifest != nil {
		m, e := s.Contexts.Read(ctx, access.Principal{UserID: deviceOwner(ctx, s.Store.Database(), w, p.DeviceID)}, w, *spec.Manifest)
		if e != nil {
			return AssignedInput{}, e
		}
		result.Manifest = &m
	}
	return result, nil
}
func (s *Service) registerAssignment(mux *http.ServeMux) {
	base := "/runs/workspaces/{workspace}"
	mux.HandleFunc("POST "+base+"/runner", func(w http.ResponseWriter, r *http.Request) {
		ws := r.PathValue("workspace")
		p, err := s.Access.DevicePrincipal(r.Context(), ws, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		var value any
		if err == nil {
			var req RunnerContact
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				value, err = s.ContactRunner(r.Context(), p, ws, req)
			}
		}
		reply(w, ws, value, err)
	})
	mux.HandleFunc("GET "+base+"/runner/{device}", func(w http.ResponseWriter, r *http.Request) {
		ws := r.PathValue("workspace")
		p, ok := s.principal(w, r)
		if !ok {
			return
		}
		err := s.Access.Check(r.Context(), p, ws, "", "read")
		var value any
		if err == nil {
			value, err = runner(r.Context(), s.Store.Database(), ws, r.PathValue("device"))
			if err == nil {
				err = s.Access.Check(r.Context(), p, ws, "runner:"+r.PathValue("device"), "get")
			}
			if errors.Is(err, pgx.ErrNoRows) {
				value = Runner{Ref: Ref{ID: "runner:" + r.PathValue("device"), Version: "0"}, DeviceID: r.PathValue("device"), Connections: []Connection{}}
				err = nil
			}
		}
		reply(w, ws, value, err)
	})
	mux.HandleFunc("GET "+base+"/executions/{id}/assigned-input", func(w http.ResponseWriter, r *http.Request) {
		ws := r.PathValue("workspace")
		p, err := s.Access.DevicePrincipal(r.Context(), ws, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		var value any
		if err == nil {
			value, err = s.Assigned(r.Context(), p, ws, r.PathValue("id"), r.URL.Query().Get("generation"))
		}
		reply(w, ws, value, err)
	})
}
