package workflows

import (
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"net/http"
)

func (s *Service) registerExecution(mux *http.ServeMux) {
	base := "/workflows/workspaces/{workspace}"
	mux.HandleFunc("GET "+base+"/executions/{id}/export", s.exportHTTP)
	mux.HandleFunc("POST "+base+"/executions/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		p := access.Principal{UserID: user}
		ws, id := r.PathValue("workspace"), r.PathValue("id")
		var v any
		var err error
		switch r.PathValue("action") {
		case "start":
			var req Start
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Start(r.Context(), p, ws, id, req)
			}
		case "decide":
			var req Decision
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Decide(r.Context(), p, ws, id, req)
			}
		case "preview":
			var req Preview
			if !body(r, &req) || req.Execution.ID != id {
				err = ErrInvalid
			} else {
				v, err = s.Preview(r.Context(), p, ws, req)
			}
		case "impact":
			var req ImpactRequest
			if !body(r, &req) || req.Execution.ID != id {
				err = ErrInvalid
			} else {
				v, err = s.Impact(r.Context(), p, ws, req)
			}
		case "apply":
			var req Apply
			if !body(r, &req) {
				err = ErrInvalid
			} else {
				v, err = s.Apply(r.Context(), p, ws, id, req)
			}
		default:
			err = ErrInvalid
		}
		reply(w, ws, v, err)
	})
}
