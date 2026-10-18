package clientops

import "net/http"

func (s *Service) registerGeneration(mux *http.ServeMux) {
	root := "/client-operations/workspaces/{workspace}/generation"
	mux.HandleFunc("GET "+root+"/results/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		v, err := s.GenerationResultPage(r.Context(), p, r.PathValue("workspace"), Ref{ID: r.PathValue("id"), Version: r.URL.Query().Get("version")}, r.URL.Query().Get("after"))
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/preview", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var source GenerationSource
		if !body(r, &source) {
			response(w, nil, ErrInvalid)
			return
		}
		v, _, err := s.generationSource(r.Context(), p, r.PathValue("workspace"), &GenerationPolicy{Source: source, Fixed: []ContextLock{}, GroupScopes: nil})
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req Write
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.PutGenerationRule(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/activate", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req EnableGeneration
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.EnableGeneration(r.Context(), p, r.PathValue("workspace"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/backfills/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req GenerationBackfill
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.BackfillGeneration(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
	mux.HandleFunc("POST "+root+"/next", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req struct {
			Client string `json:"client"`
		}
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.NextGeneration(r.Context(), p, r.PathValue("workspace"), req.Client)
		response(w, v, err)
	})
	mux.HandleFunc("GET "+root+"/queue", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		v, err := s.GenerationQueue(r.Context(), p, r.PathValue("workspace"), r.URL.Query().Get("after"))
		response(w, v, err)
	})
}
