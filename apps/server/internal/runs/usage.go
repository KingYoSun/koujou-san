package runs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

type usageReport struct {
	usage.Write
	Generation   string `json:"generation"`
	ObservedAtMS *int64 `json:"observed_at_ms,omitempty"`
}

func (s *Service) registerUsage(mux *http.ServeMux) {
	u := usage.New(s.Store, s.Access)
	mux.HandleFunc("POST /runs/workspaces/{workspace}/executions/{id}/usage", func(w http.ResponseWriter, r *http.Request) {
		workspace, id := r.PathValue("workspace"), runID(r.PathValue("id"))
		p, err := s.Access.DevicePrincipal(r.Context(), workspace, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			reply(w, workspace, nil, access.ErrForbidden)
			return
		}
		var req usageReport
		if !body(r, &req) {
			reply(w, workspace, nil, ErrInvalid)
			return
		}
		var o usage.Observation
		if json.Unmarshal(req.Value, &o) != nil || o.Measurement == nil {
			reply(w, workspace, nil, ErrInvalid)
			return
		}
		ctx := r.Context()
		var deps []usage.Ref
		var owner string
		check := func(ctx context.Context, tx pgx.Tx) error {
			var q query = s.Store.Database()
			if tx != nil {
				q = tx
			}
			if tx != nil {
				err = s.Access.CheckTx(ctx, tx, p, workspace, id, "report")
			} else {
				err = s.Access.Check(ctx, p, workspace, id, "report")
			}
			if err != nil {
				return err
			}
			current, _, e := loadRun(ctx, q, workspace, id)
			if e != nil || current.Assignment == nil || current.StartedAt == nil {
				return access.ErrForbidden
			}
			if o.Connection != current.Assignment.Connection.Target {
				return ErrInvalid
			}
			if e = s.checkAssignment(ctx, q, p, workspace, current, req.Generation, true); e != nil {
				return e
			}
			if o.Attribution.Kind != "run" || o.Attribution.ID != id || o.Attribution.Resource != id || !strings.HasPrefix(o.Source, "device:"+p.DeviceID+"/") || o.Measurement.Generation != req.Generation {
				return ErrInvalid
			}
			if tx == nil {
				owner = deviceOwner(ctx, q, workspace, p.DeviceID)
				refs, e := s.inputRefs(ctx, q, workspace, current)
				if e != nil {
					return e
				}
				deps = []usage.Ref{{ID: id}}
				for _, ref := range refs {
					deps = append(deps, usage.Ref{ID: ref.ID, Version: ref.Version})
				}
			}
			return nil
		}
		if err = check(ctx, nil); err != nil {
			reply(w, workspace, nil, err)
			return
		}
		if req.ObservedAtMS != nil {
			o.At = time.UnixMilli(*req.ObservedAtMS).UTC()
			req.Value, _ = json.Marshal(o)
		}
		value, err := u.PutFromRun(ctx, access.Principal{UserID: owner}, workspace, usage.ObservationID(o), req.Write, usage.RunWrite{Actor: "device:" + p.DeviceID, Dependencies: deps, Check: check})
		if err == usage.ErrInvalid {
			err = ErrInvalid
		}
		reply(w, workspace, value, err)
	})
}
