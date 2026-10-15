package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, err error) {
	status := 503
	code := "authentication_unavailable"
	if errors.Is(err, ErrUnauthorized) {
		status = 401
		code = "authentication_required"
	}
	reply(w, status, map[string]string{"error": code})
}
func cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func cookieValue(r *http.Request, name string) string {
	value, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return value.Value
}
func (s *Service) csrf(w http.ResponseWriter, r *http.Request) bool {
	value := cookieValue(r, "__Host-koujou-csrf")
	if r.Header.Get("Origin") != s.Config.Site || value == "" || subtle.ConstantTimeCompare([]byte(value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		reply(w, 403, map[string]string{"error": "invalid_csrf"})
		return false
	}
	return true
}

// RequestUser applies the same identity and browser CSRF boundary to domain APIs.
func (s *Service) RequestUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	if r.Method != "GET" && r.Method != "HEAD" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && !s.csrf(w, r) {
		return User{}, false
	}
	tokens, err := s.requestTokens(r)
	if err != nil {
		failure(w, err)
		return User{}, false
	}
	user, err := s.User(r.Context(), tokens.Access)
	if err != nil {
		failure(w, err)
		return User{}, false
	}
	return user, true
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/config", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"issuer": s.Config.Issuer, "client_id": s.Config.NativeClient, "redirect_uri": s.Config.NativeRedirect, "api_url": s.Config.API, "site_url": s.Config.Site, "audience": s.Config.Audience})
	})
	mux.HandleFunc("GET /auth/csrf", func(w http.ResponseWriter, r *http.Request) {
		value := cookieValue(r, "__Host-koujou-csrf")
		if value == "" {
			value = rand.Text()
			cookie(w, "__Host-koujou-csrf", value, 86400)
		}
		reply(w, 200, map[string]string{"csrf": value})
	})
	mux.HandleFunc("GET /auth/start", s.start)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
		tokens, err := s.requestTokens(r)
		if err != nil {
			failure(w, err)
			return
		}
		user, err := s.User(r.Context(), tokens.Access)
		if err != nil {
			failure(w, err)
			return
		}
		reply(w, 200, map[string]any{"user": user})
	})
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("POST /auth/broker/{kind}", s.broker)
	mux.HandleFunc("GET /auth/flow/{kind}", s.flow)
	mux.HandleFunc("POST /auth/flow/{kind}", s.flow)
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	state, nonce, verifier := rand.Text(), rand.Text(), rand.Text()+rand.Text()
	_, err := s.DB.Exec(r.Context(), "INSERT INTO auth_attempts VALUES($1,$2,$3,$4)", hash(state), s.seal([]byte(verifier)), nonce, time.Now().Add(15*time.Minute))
	if err != nil {
		failure(w, err)
		return
	}
	cookie(w, "__Host-koujou-login", state, 900)
	query := url.Values{"client_id": {s.Config.WebClient}, "redirect_uri": {s.Config.Site + "/auth/callback"}, "response_type": {"code"}, "scope": {"openid offline_access email profile koujou"}, "audience": {s.Config.Audience}, "state": {state}, "nonce": {nonce}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}}
	http.Redirect(w, r, s.Config.Issuer+"oauth2/auth?"+query.Encode(), 303)
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookieValue(r, "__Host-koujou-login"))) != 1 {
		failure(w, ErrUnauthorized)
		return
	}
	var encrypted []byte
	var nonce string
	err := s.DB.QueryRow(r.Context(), "DELETE FROM auth_attempts WHERE state=$1 AND expires_at>now() RETURNING verifier,nonce", hash(state)).Scan(&encrypted, &nonce)
	if err != nil {
		failure(w, ErrUnauthorized)
		return
	}
	verifier, err := s.open(encrypted)
	if err != nil {
		failure(w, ErrUnauthorized)
		return
	}
	tokens, err := s.exchange(r.Context(), url.Values{"grant_type": {"authorization_code"}, "code": {r.URL.Query().Get("code")}, "redirect_uri": {s.Config.Site + "/auth/callback"}, "code_verifier": {string(verifier)}}, true)
	if err != nil {
		failure(w, err)
		return
	}
	id, err := s.verifier.Verify(r.Context(), tokens.ID)
	if err != nil || id.Nonce != nonce {
		failure(w, ErrUnauthorized)
		return
	}
	info, err := s.Inspect(r.Context(), tokens.Access)
	if err != nil || info.Subject != id.Subject {
		failure(w, ErrUnauthorized)
		return
	}
	if _, err = s.User(r.Context(), tokens.Access); err != nil {
		failure(w, err)
		return
	}
	session := rand.Text()
	raw, err := json.Marshal(tokens)
	if err != nil {
		failure(w, err)
		return
	}
	_, err = s.DB.Exec(r.Context(), "INSERT INTO auth_sessions VALUES($1,$2,$3)", hash(session), s.seal(raw), time.Now().Add(7*24*time.Hour))
	if err != nil {
		failure(w, err)
		return
	}
	cookie(w, "__Host-koujou-session", session, 7*86400)
	cookie(w, "__Host-koujou-login", "", -1)
	http.Redirect(w, r, s.Config.Site+"/", 303)
}

func (s *Service) requestTokens(r *http.Request) (Tokens, error) {
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		return Tokens{Access: strings.TrimPrefix(value, "Bearer ")}, nil
	}
	id := cookieValue(r, "__Host-koujou-session")
	if id == "" {
		return Tokens{}, ErrUnauthorized
	}
	var tokens Tokens
	err := pgx.BeginFunc(r.Context(), s.DB, func(tx pgx.Tx) error {
		var encrypted []byte
		if err := tx.QueryRow(r.Context(), "SELECT tokens FROM auth_sessions WHERE id=$1 AND expires_at>now() FOR UPDATE", hash(id)).Scan(&encrypted); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnauthorized
			}
			return err
		}
		raw, err := s.open(encrypted)
		if err != nil {
			return ErrUnauthorized
		}
		if err = json.Unmarshal(raw, &tokens); err != nil {
			return ErrUnauthorized
		}
		if time.Until(tokens.Expires) > 15*time.Second {
			return nil
		}
		tokens, err = s.exchange(r.Context(), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tokens.Refresh}}, true)
		if err != nil {
			return err
		}
		raw, err = json.Marshal(tokens)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), "UPDATE auth_sessions SET tokens=$2 WHERE id=$1", hash(id), s.seal(raw))
		return err
	})
	return tokens, err
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if !s.csrf(w, r) {
		return
	}
	tokens, err := s.requestTokens(r)
	oauth := "unconfirmed"
	if err == nil {
		refreshErr := s.Revoke(r.Context(), tokens.Refresh, true)
		accessErr := s.Revoke(r.Context(), tokens.Access, true)
		if refreshErr == nil && accessErr == nil {
			oauth = "revoked"
		}
	}
	if _, err = s.DB.Exec(r.Context(), "DELETE FROM auth_sessions WHERE id=$1", hash(cookieValue(r, "__Host-koujou-session"))); err != nil {
		failure(w, err)
		return
	}
	cookie(w, "__Host-koujou-session", "", -1)
	kratos := "unconfirmed"
	request, _ := http.NewRequestWithContext(r.Context(), "GET", s.Config.KratosPublic+"/self-service/logout/browser", nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", r.Header.Get("Cookie"))
	response, requestErr := s.HTTP.Do(request)
	if requestErr == nil {
		var value struct {
			Token string `json:"logout_token"`
			URL   string `json:"logout_url"`
		}
		_ = json.NewDecoder(response.Body).Decode(&value)
		_ = response.Body.Close()
		if value.Token == "" {
			target, _ := url.Parse(value.URL)
			if target != nil {
				value.Token = target.Query().Get("token")
			}
		}
		if value.Token != "" {
			request, _ = http.NewRequestWithContext(r.Context(), "GET", s.Config.KratosPublic+"/self-service/logout?token="+url.QueryEscape(value.Token), nil)
			request.Header.Set("Cookie", r.Header.Get("Cookie"))
			response, requestErr = s.HTTP.Do(request)
			if requestErr == nil {
				copyCookies(w, response)
				_ = response.Body.Close()
				if response.StatusCode < 400 {
					kratos = "ended"
				}
			}
		}
	}
	reply(w, 200, map[string]string{"app_session": "ended", "oauth_tokens": oauth, "kratos_session": kratos})
}

func (s *Service) broker(w http.ResponseWriter, r *http.Request) {
	if !s.csrf(w, r) {
		return
	}
	kind := r.PathValue("kind")
	if kind != "login" && kind != "consent" {
		http.NotFound(w, r)
		return
	}
	challenge := r.URL.Query().Get("challenge")
	if challenge == "" {
		failure(w, ErrUnauthorized)
		return
	}
	var session KratosSession
	status, err := s.call(r.Context(), "GET", s.Config.KratosPublic+"/sessions/whoami", nil, r.Header.Get("Cookie"), &session)
	if err != nil {
		failure(w, err)
		return
	}
	if status == 403 {
		reply(w, 200, map[string]string{"required": "aal2"})
		return
	}
	if status != 200 || !session.Active {
		reply(w, 200, map[string]string{"required": "login"})
		return
	}
	if !verified(session.Identity) {
		reply(w, 200, map[string]string{"required": "verification"})
		return
	}
	var value struct {
		Client struct {
			ID string `json:"client_id"`
		} `json:"client"`
		Subject  string   `json:"subject"`
		Scopes   []string `json:"requested_scope"`
		Audience []string `json:"requested_access_token_audience"`
	}
	endpoint := s.Config.HydraAdmin + "/admin/oauth2/auth/requests/" + kind + "?" + kind + "_challenge=" + url.QueryEscape(challenge)
	status, err = s.call(r.Context(), "GET", endpoint, nil, "", &value)
	if err != nil {
		failure(w, err)
		return
	}
	if status != 200 || (value.Client.ID != s.Config.WebClient && value.Client.ID != s.Config.NativeClient) {
		failure(w, ErrUnauthorized)
		return
	}
	var accept map[string]any
	if kind == "login" {
		accept = map[string]any{"subject": session.Identity.ID, "remember": false, "context": map[string]string{"aal": session.AAL}}
	} else {
		if value.Subject != session.Identity.ID {
			failure(w, ErrUnauthorized)
			return
		}
		for _, scope := range value.Scopes {
			if !strings.Contains(" openid offline_access email profile koujou ", " "+scope+" ") {
				failure(w, ErrUnauthorized)
				return
			}
		}
		for _, audience := range value.Audience {
			if audience != s.Config.Audience {
				failure(w, ErrUnauthorized)
				return
			}
		}
		accept = map[string]any{"grant_scope": value.Scopes, "grant_access_token_audience": value.Audience, "remember": false, "session": map[string]any{"access_token": map[string]string{"aal": session.AAL}, "id_token": map[string]any{"email": session.Identity.Traits.Email, "email_verified": true}}}
	}
	var redirect struct {
		To string `json:"redirect_to"`
	}
	target := s.Config.HydraAdmin + "/admin/oauth2/auth/requests/" + kind + "/accept?" + kind + "_challenge=" + url.QueryEscape(challenge)
	status, err = s.call(r.Context(), "PUT", target, accept, "", &redirect)
	if err != nil || status >= 400 || !sameOrigin(redirect.To, s.Config.Issuer) {
		failure(w, errors.New("invalid_authorization_response"))
		return
	}
	reply(w, 200, map[string]string{"redirect": redirect.To})
}

func sameOrigin(target, base string) bool {
	left, a := url.Parse(target)
	right, b := url.Parse(base)
	return a == nil && b == nil && left.Scheme == right.Scheme && left.Host == right.Host
}
func copyCookies(w http.ResponseWriter, response *http.Response) {
	for _, value := range response.Cookies() {
		value.Secure = true
		value.HttpOnly = true
		value.Domain = ""
		value.Path = "/"
		value.SameSite = http.SameSiteLaxMode
		http.SetCookie(w, value)
	}
}

type Flow struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	State    string `json:"state,omitempty"`
	ReturnTo string `json:"return_to,omitempty"`
	UI       struct {
		Action   string            `json:"action"`
		Method   string            `json:"method"`
		Nodes    []json.RawMessage `json:"nodes"`
		Messages []json.RawMessage `json:"messages,omitempty"`
	} `json:"ui"`
}

func (s *Service) flow(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "login" && kind != "registration" && kind != "settings" && kind != "recovery" && kind != "verification" {
		http.NotFound(w, r)
		return
	}
	flowID := r.URL.Query().Get("flow")
	target := s.Config.KratosPublic + "/self-service/" + kind + "/browser"
	if flowID != "" {
		target = s.Config.KratosPublic + "/self-service/" + kind + "/flows?id=" + url.QueryEscape(flowID)
	} else {
		returnTo := s.Config.Site + "/auth/start"
		if kind == "recovery" {
			returnTo = s.Config.Site + "/auth/settings"
		}
		if target := r.URL.Query().Get("return_to"); target != "" && sameOrigin(target, s.Config.Site) {
			returnTo = target
		}
		if challenge := r.URL.Query().Get("login_challenge"); challenge != "" {
			returnTo = s.Config.Site + "/auth/login?login_challenge=" + url.QueryEscape(challenge)
		}
		query := url.Values{"return_to": {returnTo}}
		if kind == "login" && r.URL.Query().Get("aal") == "aal2" {
			query.Set("aal", "aal2")
		}
		target += "?" + query.Encode()
	}
	request, _ := http.NewRequestWithContext(r.Context(), "GET", target, nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", r.Header.Get("Cookie"))
	response, err := s.HTTP.Do(request)
	if err != nil {
		failure(w, err)
		return
	}
	copyCookies(w, response)
	if response.StatusCode == 303 && sameOrigin(response.Header.Get("Location"), s.Config.Site) {
		_ = response.Body.Close()
		reply(w, 200, map[string]string{"redirect": response.Header.Get("Location")})
		return
	}
	var flow Flow
	decodeErr := json.NewDecoder(response.Body).Decode(&flow)
	_ = response.Body.Close()
	if response.StatusCode == 403 {
		reply(w, 403, map[string]string{"error": "aal2_required"})
		return
	}
	if response.StatusCode >= 400 || decodeErr != nil || flow.Type != "browser" {
		failure(w, ErrUnauthorized)
		return
	}
	if r.Method == "GET" {
		flow.UI.Action = s.Config.Site + "/auth/flow/" + kind + "?flow=" + url.QueryEscape(flow.ID)
		reply(w, 200, flow)
		return
	}
	if r.Header.Get("Origin") != s.Config.Site {
		reply(w, 403, map[string]string{"error": "invalid_origin"})
		return
	}
	request, _ = http.NewRequestWithContext(r.Context(), "POST", s.Config.KratosPublic+"/self-service/"+kind+"?flow="+url.QueryEscape(flow.ID), r.Body)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Cookie", r.Header.Get("Cookie"))
	request.Header.Set("Origin", s.Config.Site)
	response, err = s.HTTP.Do(request)
	if err != nil {
		failure(w, err)
		return
	}
	defer response.Body.Close()
	copyCookies(w, response)
	location := response.Header.Get("Location")
	if location != "" && sameOrigin(location, s.Config.Site) {
		http.Redirect(w, r, location, 303)
		return
	}
	// Validation errors remain attached to this browser flow and are rendered on GET.
	redirect := s.Config.Site + "/auth/" + kind + "?flow=" + url.QueryEscape(flow.ID)
	if strings.Contains(flow.ReturnTo, "login_challenge=") {
		parsed, _ := url.Parse(flow.ReturnTo)
		redirect += "&login_challenge=" + url.QueryEscape(parsed.Query().Get("login_challenge"))
	}
	http.Redirect(w, r, redirect, 303)
}
