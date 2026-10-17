package runs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type RunnerConnection struct {
	apicontract.Mutation
	Generation string `json:"generation"`
	Unknown    bool   `json:"unknown"`
}

// Transport observation and lease renewal do not consume a CLI event sequence.
func (s *Service) Connection(ctx context.Context, p access.Principal, w, id string, req RunnerConnection) (apicontract.Response[apicontract.Reference], error) {
	if p.DeviceID == "" || req.Generation == "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	return s.mutate(ctx, p, w, id, "report", "run-connection", req.Mutation, req, func(tx pgx.Tx, ref apicontract.Reference) (Run, []Ref, error) {
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return r, nil, err
		}
		if terminal(r.State) || r.Assignment == nil {
			return r, nil, ErrState
		}
		r.Assignment.ExpiresAt = time.Now().UTC().Add(assignmentLifetime)
		if req.Unknown {
			r.Observation = "unknown"
		}
		return r, old.Refs, nil
	}, func(q query) error {
		r, _, err := loadRun(ctx, q, w, id)
		if err != nil {
			return err
		}
		return s.checkAssignment(ctx, q, p, w, r, req.Generation, true)
	})
}

type RunnerState struct {
	Run   Ref    `json:"run"`
	Value Run    `json:"value"`
	Pause *Pause `json:"pause,omitempty"`
}
type RunnerQueue struct {
	Items []RunnerState `json:"items"`
	Next  string        `json:"next"`
}

// State observation permits an expired assignment, but grants no new process
// start. assigned-input remains the current authorization check before launch.
func (s *Service) RunnerState(ctx context.Context, p access.Principal, w, id, generation string) (RunnerState, error) {
	if p.DeviceID == "" {
		return RunnerState{}, access.ErrForbidden
	}
	if err := s.Access.Check(ctx, p, w, runID(id), "input"); err != nil {
		return RunnerState{}, err
	}
	r, rec, err := loadRun(ctx, s.Store.Database(), w, id)
	if err != nil {
		return RunnerState{}, err
	}
	if r.Assignment == nil {
		return RunnerState{}, access.ErrForbidden
	}
	if err = s.checkAssignment(ctx, s.Store.Database(), p, w, r, generation, true); err != nil {
		return RunnerState{}, err
	}
	out := RunnerState{Run: rec.Ref, Value: r}
	if r.ActivePause != nil {
		pr, e := record(ctx, s.Store.Database(), w, *r.ActivePause)
		if e != nil {
			return RunnerState{}, e
		}
		owner := access.Principal{UserID: deviceOwner(ctx, s.Store.Database(), w, p.DeviceID)}
		if e = s.refs(ctx, s.Store.Database(), owner, w, append(pr.Refs, pr.Ref)); e != nil {
			return RunnerState{}, e
		}
		var pause Pause
		if e = json.Unmarshal(pr.Value, &pause); e != nil {
			return RunnerState{}, e
		}
		out.Pause = &pause
	}
	return out, nil
}
func (s *Service) runnerQueue(ctx context.Context, p access.Principal, w, after string) (RunnerQueue, error) {
	if err := s.Access.Check(ctx, p, w, "", "contact"); err != nil {
		return RunnerQueue{}, err
	}
	var allowed bool
	if err := s.Store.Database().QueryRow(ctx, `SELECT can_input AND can_report FROM access_devices WHERE workspace_id=$1 AND id=$2`, w, p.DeviceID).Scan(&allowed); err != nil || !allowed {
		return RunnerQueue{}, access.ErrForbidden
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT h.id,h.version,h.payload FROM access_resources a
 JOIN shared_versions v ON v.workspace_id=a.workspace_id AND v.resource_id=a.id
 JOIN run_records h ON h.workspace_id=v.workspace_id AND h.id=v.resource_id AND h.version=v.version
 WHERE a.workspace_id=$1 AND a.assigned_device=$2 AND a.id LIKE 'run:%' AND a.id>$3
 AND h.payload->'assignment' IS NOT NULL AND h.payload->>'state' IN ('queued','running','paused')
 ORDER BY a.id LIMIT 50`, w, p.DeviceID, after)
	if err != nil {
		return RunnerQueue{}, err
	}
	defer rows.Close()
	result := RunnerQueue{Items: []RunnerState{}}
	count := 0
	last := ""
	for rows.Next() {
		var r RunnerState
		var payload []byte
		if err = rows.Scan(&r.Run.ID, &r.Run.Version, &payload); err != nil {
			return result, err
		}
		if err = json.Unmarshal(payload, &r.Value); err != nil {
			return result, err
		}
		count++
		last = r.Run.ID
		a := r.Value.Assignment
		if a != nil && s.checkAssignment(ctx, s.Store.Database(), p, w, r.Value, a.Generation, true) == nil {
			result.Items = append(result.Items, r)
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if count == 50 {
		result.Next = last
	}
	return result, nil
}
func (s *Service) registerRunner(mux *http.ServeMux) {
	base := "/runs/workspaces/{workspace}"
	device := func(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
		ws := r.PathValue("workspace")
		p, err := s.Access.DevicePrincipal(r.Context(), ws, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			reply(w, ws, nil, err)
			return p, false
		}
		return p, true
	}
	mux.HandleFunc("POST "+base+"/executions/{id}/connection", func(w http.ResponseWriter, r *http.Request) {
		p, ok := device(w, r)
		if !ok {
			return
		}
		ws := r.PathValue("workspace")
		var req RunnerConnection
		if !body(r, &req) {
			reply(w, ws, nil, ErrInvalid)
			return
		}
		value, err := s.Connection(r.Context(), p, ws, r.PathValue("id"), req)
		reply(w, ws, value, err)
	})
	mux.HandleFunc("GET "+base+"/runner/self", func(w http.ResponseWriter, r *http.Request) {
		p, ok := device(w, r)
		if !ok {
			return
		}
		ws := r.PathValue("workspace")
		if err := s.Access.Check(r.Context(), p, ws, "", "contact"); err != nil {
			reply(w, ws, nil, err)
			return
		}
		value, err := runner(r.Context(), s.Store.Database(), ws, p.DeviceID)
		if errors.Is(err, pgx.ErrNoRows) {
			value = Runner{Ref: Ref{ID: "runner:" + p.DeviceID, Version: "0"}, DeviceID: p.DeviceID, Connections: []Connection{}}
			err = nil
		}
		reply(w, ws, value, err)
	})
	mux.HandleFunc("GET "+base+"/runner/assignments", func(w http.ResponseWriter, r *http.Request) {
		p, ok := device(w, r)
		if !ok {
			return
		}
		ws := r.PathValue("workspace")
		result, err := s.runnerQueue(r.Context(), p, ws, r.URL.Query().Get("after"))
		reply(w, ws, result, err)
	})
	mux.HandleFunc("GET "+base+"/executions/{id}/runner-state", func(w http.ResponseWriter, r *http.Request) {
		p, ok := device(w, r)
		if !ok {
			return
		}
		ws := r.PathValue("workspace")
		result, err := s.RunnerState(r.Context(), p, ws, r.PathValue("id"), r.URL.Query().Get("generation"))
		reply(w, ws, result, err)
	})
}
