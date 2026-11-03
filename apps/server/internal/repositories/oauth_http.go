package repositories

import (
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"net/http"
)

func principal(user string) access.Principal { return access.Principal{UserID: user} }
func (s *Service) registerOAuth(mux *http.ServeMux) {
	body := func(w http.ResponseWriter, r *http.Request, value any) bool {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if d.Decode(value) != nil {
			send(w, nil, ErrInvalid)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /forge/oauth/apps", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		value, err := s.OAuthApps(r.Context(), principal(p), "", r.URL.Query().Get("after"))
		callback, _ := s.callbackURL()
		send(w, struct {
			Page[OAuthApp]
			Callback string `json:"callback_url"`
		}{value, callback}, err)
	})
	mux.HandleFunc("POST /forge/oauth/apps/{app}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req OAuthAppWrite
		if !body(w, r, &req) {
			return
		}
		value, err := s.PutOAuthApp(r.Context(), principal(p), r.PathValue("app"), req)
		send(w, value, err)
	})
	mux.HandleFunc("GET /forge/workspaces/{workspace}/oauth/apps", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		value, err := s.OAuthApps(r.Context(), principal(p), r.PathValue("workspace"), r.URL.Query().Get("after"))
		send(w, value, err)
	})
	mux.HandleFunc("POST /forge/workspaces/{workspace}/oauth/{app}/start", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req OAuthStart
		if !body(w, r, &req) {
			return
		}
		value, err := s.StartOAuth(r.Context(), principal(p), r.PathValue("workspace"), r.PathValue("app"), req)
		send(w, value, err)
	})
	mux.HandleFunc("GET /forge/workspaces/{workspace}/oauth/sessions/{session}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		value, err := s.OAuthStatus(r.Context(), principal(p), r.PathValue("workspace"), r.PathValue("session"))
		send(w, value, err)
	})
	mux.HandleFunc("POST /forge/workspaces/{workspace}/oauth/sessions/{session}/accept", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		var req OAuthAccept
		if !body(w, r, &req) {
			return
		}
		value, err := s.AcceptOAuth(r.Context(), principal(p), r.PathValue("workspace"), r.PathValue("session"), req)
		send(w, value, err)
	})
	mux.HandleFunc("GET /forge/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		err := s.CompleteOAuth(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if err != nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte("<!doctype html><html lang=ja><title>Gitea認証</title><p>Gitea認証を確認できません。接続画面から認証をやり直してください。</p>"))
			return
		}
		_, _ = w.Write([]byte("<!doctype html><html lang=ja><title>Gitea認証</title><p>Gitea認証を受け付けました。</p>"))
	})
}
