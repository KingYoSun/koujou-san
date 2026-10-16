package usage

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
)

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func result(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status, code := 503, "unavailable"
		if errors.Is(err, access.ErrForbidden) {
			status, code = 403, "forbidden"
		}
		if errors.Is(err, ErrInvalid) {
			status, code = 400, "invalid_request"
		}
		send(w, status, map[string]string{"error": code})
		return
	}
	send(w, 200, value)
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /usage/workspaces/{workspace}/query", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Request
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			result(w, nil, ErrInvalid)
			return
		}
		v, err := s.Query(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), req)
		result(w, v, err)
	})
	mux.HandleFunc("GET /usage/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Read(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: version})
		result(w, v, err)
	})
	mux.HandleFunc("POST /usage/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Write
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil {
			result(w, nil, ErrInvalid)
			return
		}
		v, err := s.Put(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"), req)
		if err == nil && v.Error != nil {
			send(w, 409, v)
			return
		}
		result(w, v, err)
	})
}
