package clientops

import "net/http"

func (s *Service) registerChat(mux *http.ServeMux) {
	root := "/client-operations/workspaces/{workspace}/chat"
	for _, kind := range []string{"threads", "messages", "links"} {
		mux.HandleFunc("POST "+root+"/"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			p, ok := s.user(w, r)
			if !ok {
				return
			}
			var req Write
			if !body(r, &req) {
				response(w, nil, ErrInvalid)
				return
			}
			write := s.PutChatThread
			if kind == "messages" {
				write = s.PutChatMessage
			} else if kind == "links" {
				write = s.PutChatLink
			}
			v, err := write(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
			response(w, v, err)
		})
	}
	mux.HandleFunc("POST "+root+"/adoptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req ChatAdopt
		if !body(r, &req) {
			response(w, nil, ErrInvalid)
			return
		}
		v, err := s.AdoptChat(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), req)
		response(w, v, err)
	})
}
