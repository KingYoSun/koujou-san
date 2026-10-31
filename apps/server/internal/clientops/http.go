package clientops

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func response(w http.ResponseWriter, v any, err error) {
	if err != nil {
		code, status := "unavailable", 503
		if errors.Is(err, access.ErrForbidden) {
			code, status = "forbidden", 403
		} else if errors.Is(err, ErrInvalid) {
			code, status = "invalid_request", 400
		} else if errors.Is(err, ErrState) {
			code, status = "state_changed", 409
		}
		send(w, status, map[string]string{"error": code})
		return
	}
	if out, ok := v.(apicontract.Response[apicontract.Reference]); ok && out.Error != nil {
		status := 409
		if out.Error.Code == "forbidden" {
			status = 403
		}
		send(w, status, v)
		return
	}
	send(w, 200, v)
}
func (s *Service) Register(mux *http.ServeMux) {
	root := "/client-operations/workspaces/{workspace}"
	s.registerGeneration(mux)
	s.registerChat(mux)
	mux.HandleFunc("GET "+root+"/origins/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		v, err := s.GenerationOrigin(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), r.URL.Query().Get("version"), r.URL.Query().Get("operation"))
		response(w, v, err)
	})
	mux.HandleFunc("GET "+root+"/template", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		if err := s.Access.Check(r.Context(), p, r.PathValue("workspace"), "", "read"); err != nil {
			response(w, nil, err)
			return
		}
		send(w, 200, DefaultPolicy())
	})
	mux.HandleFunc("GET "+root+"/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Read(r.Context(), p, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: version})
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/query", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Request
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.Query(r.Context(), p, r.PathValue("workspace"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/policies/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Write
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.PutPolicy(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/selections/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req SelectionWrite
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.PutSelection(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Start
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.Start(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/operations/{id}/actions", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Action
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.Act(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/operations/{id}/begin", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Begin
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.Begin(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/operations/{id}/finish", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Finish
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.Finish(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("GET "+root+"/operations/{id}/transport", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		workspace := r.PathValue("workspace")
		v, err := s.Read(r.Context(), p, workspace, Ref{ID: r.PathValue("id"), Version: "head"})
		if err != nil {
			response(w, nil, err)
			return
		}
		var op Operation
		if decode(v.Value, &op) != nil || op.User != p.UserID {
			response(w, nil, access.ErrForbidden)
			return
		}
		send(w, 200, map[string]string{"handle": s.Access.OperationHandle(workspace, op.ReadOperation, p.UserID), "operation": op.ReadOperation})
	})
}
func body(r *http.Request, v any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v) == nil
}
func (s *Service) user(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
	id, ok := s.Access.User(w, r)
	return access.Principal{UserID: id}, ok
}
