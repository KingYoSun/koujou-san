package auth

import (
	"net/http"
)

type resourceFlow struct{ login, session, callback, scope, audience, requiredScope string }

func (s *Service) resourceFlow(mcp bool) resourceFlow {
	if mcp {
		return resourceFlow{"__Host-koujou-mcp-login", "__Host-koujou-mcp-session", s.Config.Site + "/auth/mcp/callback", "openid offline_access email profile koujou.mcp", s.Config.MCPResource(), "koujou.mcp"}
	}
	return resourceFlow{"__Host-koujou-login", "__Host-koujou-session", s.Config.Site + "/auth/callback", "openid offline_access email profile koujou", s.Config.Audience, "koujou"}
}
func (s *Service) registerMCPClient(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/mcp/config", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"issuer": s.Config.Issuer, "client_id": s.Config.NativeClient, "redirect_uri": s.Config.NativeRedirect, "api_url": s.Config.API, "site_url": s.Config.Site, "audience": s.Config.MCPResource()})
	})
	mux.HandleFunc("GET /auth/mcp/start", func(w http.ResponseWriter, r *http.Request) { s.startResource(w, r, true) })
	mux.HandleFunc("GET /auth/mcp/callback", func(w http.ResponseWriter, r *http.Request) { s.callbackResource(w, r, true) })
	mux.HandleFunc("GET /auth/mcp/me", func(w http.ResponseWriter, r *http.Request) {
		tokens, err := s.requestTokensNamed(r, s.resourceFlow(true).session)
		if err != nil {
			failure(w, err)
			return
		}
		user, _, err := s.MCPUser(r.Context(), tokens.Access)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, map[string]any{"user": user})
	})
}

// MCPTransport keeps the resource token in the BFF. The client still controls
// the MCP/LLM loop; this handler only attaches the dedicated OAuth credential.
func (s *Service) MCPTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.csrf(w, r) {
			return
		}
		tokens, err := s.requestTokensNamed(r, s.resourceFlow(true).session)
		if err != nil {
			failure(w, err)
			return
		}
		if _, _, err = s.MCPUser(r.Context(), tokens.Access); err != nil {
			failure(w, err)
			return
		}
		copy := r.Clone(r.Context())
		copy.Header = r.Header.Clone()
		copy.Header.Set("Authorization", "Bearer "+tokens.Access)
		next.ServeHTTP(w, copy)
	})
}
