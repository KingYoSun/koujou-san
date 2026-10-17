package hopper

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

func respond(w http.ResponseWriter, value any, err error) {
	code := http.StatusOK
	if err != nil {
		code = 503
		if errors.Is(err, access.ErrForbidden) {
			code = 403
		} else if errors.Is(err, ErrInvalid) {
			code = 400
		} else if errors.Is(err, ErrState) {
			code = 409
		}
		value = map[string]string{"error": http.StatusText(code)}
	}
	if v, ok := value.(apicontract.Response[apicontract.Reference]); ok && v.Error != nil {
		code = 409
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
func readBody(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		respond(w, nil, ErrInvalid)
		return false
	}
	return true
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /hopper/workspaces/{workspace}/template", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		err := s.Access.Check(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), "", "read")
		respond(w, Template(), err)
	})
	mux.HandleFunc("GET /hopper/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		version := r.URL.Query().Get("version")
		if version == "" {
			version = "head"
		}
		v, err := s.Read(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: version})
		respond(w, v, err)
	})
	mux.HandleFunc("POST /hopper/workspaces/{workspace}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Write
		if !readBody(w, r, &req) {
			return
		}
		v, err := s.Put(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"), req)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /hopper/workspaces/{workspace}/query", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Request
		if !readBody(w, r, &req) {
			return
		}
		v, err := s.Query(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), req)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /hopper/workspaces/{workspace}/decisions/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Decision
		if !readBody(w, r, &req) {
			return
		}
		v, err := s.Decide(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r.PathValue("id"), req)
		respond(w, v, err)
	})
	mux.HandleFunc("POST /hopper/workspaces/{workspace}/evaluate-past", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req Past
		if !readBody(w, r, &req) {
			return
		}
		v, err := s.Past(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), req)
		respond(w, v, err)
	})
}
