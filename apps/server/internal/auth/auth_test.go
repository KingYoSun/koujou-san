package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/httpapi"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

func TestOryIntegration(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_SITE") == "" {
		t.Skip("run pnpm run test:auth")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, os.Getenv("KOUJOU_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := Environment()
	cfg.Site = os.Getenv("KOUJOU_TEST_SITE")
	cfg.API = cfg.Site
	endpoint := func(name string) string { return "http://127.0.0.1:" + os.Getenv("KOUJOU_TEST_"+name+"_PORT") }
	cfg.Issuer = endpoint("HYDRA") + "/"
	cfg.HydraAdmin = endpoint("HYDRA_ADMIN")
	cfg.KratosPublic = endpoint("KRATOS")
	cfg.KratosAdmin = endpoint("KRATOS_ADMIN")
	service, err := New(ctx, cfg, store.Database())
	if err != nil {
		t.Fatal(err)
	}
	email := "member-" + strings.ToLower(rand.Text()) + "@koujou.test"
	password := "Fixture-" + rand.Text() + "aA1!"
	if err = service.Bootstrap(ctx, "admin@koujou.test", password); err != nil {
		t.Fatal(err)
	}
	if err = service.Bootstrap(ctx, "admin@koujou.test", password); err != nil {
		t.Fatal("bootstrap rerun failed", err)
	}
	var webClient map[string]any
	status, err := service.call(ctx, "GET", cfg.HydraAdmin+"/admin/clients/"+cfg.WebClient, nil, "", &webClient)
	if err != nil || status != 200 {
		t.Fatal("fixture client lookup failed")
	}
	webClient["authorization_code_grant_access_token_lifespan"] = "4s"
	status, err = service.call(ctx, "PUT", cfg.HydraAdmin+"/admin/clients/"+cfg.WebClient, webClient, "", nil)
	if err != nil || status >= 400 {
		t.Fatal("fixture access token lifetime setup failed")
	}
	server := httptest.NewUnstartedServer(httpapi.NewHandler(store.Ready, service.Register))
	_ = server.Listener.Close()
	server.Listener, err = net.Listen("tcp", "127.0.0.1:"+os.Getenv("KOUJOU_TEST_API_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(method, target string, form url.Values) (*http.Response, []byte) {
		t.Helper()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequest(method, target, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", cfg.Site)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}
	flow := func(kind, query string) (Flow, url.Values) {
		t.Helper()
		response, raw := request("GET", cfg.Site+"/auth/flow/"+kind+"?"+query, nil)
		if response.StatusCode != 200 {
			t.Fatalf("%s flow status=%d body=%s", kind, response.StatusCode, raw)
		}
		var value Flow
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		values := url.Values{}
		for _, rawNode := range value.UI.Nodes {
			var node struct {
				Attributes struct {
					Name  string `json:"name"`
					Value any    `json:"value"`
					Type  string `json:"type"`
				} `json:"attributes"`
			}
			_ = json.Unmarshal(rawNode, &node)
			if node.Attributes.Type == "hidden" {
				if text, ok := node.Attributes.Value.(string); ok {
					values.Set(node.Attributes.Name, text)
				}
			}
		}
		return value, values
	}
	submit := func(kind string, value Flow, form url.Values) (*http.Response, []byte) {
		t.Helper()
		return request("POST", cfg.Site+"/auth/flow/"+kind+"?flow="+url.QueryEscape(value.ID), form)
	}
	registration, fields := flow("registration", "")
	fields.Set("method", "password")
	fields.Set("traits.email", email)
	fields.Set("password", password)
	response, raw := submit("registration", registration, fields)
	if response.StatusCode != 303 {
		t.Fatalf("registration status=%d %s", response.StatusCode, raw)
	}
	var ids []Identity
	status, err = service.call(ctx, "GET", cfg.KratosAdmin+"/admin/identities?credentials_identifier="+url.QueryEscape(email), nil, "", &ids)
	if err != nil || status != 200 || len(ids) != 1 {
		t.Fatal("self-registration did not create one identity")
	}
	if verified(ids[0]) {
		t.Fatal("new registration was already verified")
	}
	verification, fields := flow("verification", "")
	fields.Set("method", "code")
	fields.Set("email", email)
	_, _ = submit("verification", verification, fields)
	usedMessages := map[string]bool{}
	mailCode := func(address, purpose string) string {
		t.Helper()
		for start := time.Now(); time.Since(start) < 15*time.Second; time.Sleep(100 * time.Millisecond) {
			var messages struct {
				Messages []struct {
					ID      string `json:"ID"`
					Subject string `json:"Subject"`
					To      []struct {
						Address string `json:"Address"`
					} `json:"To"`
				} `json:"messages"`
			}
			response, err := http.Get(endpoint("MAILPIT") + "/api/v1/messages")
			if err != nil {
				continue
			}
			_ = json.NewDecoder(response.Body).Decode(&messages)
			_ = response.Body.Close()
			for _, message := range messages.Messages {
				if !strings.Contains(strings.ToLower(message.Subject), purpose) {
					continue
				}
				if usedMessages[message.ID] {
					continue
				}
				for _, to := range message.To {
					if to.Address != address {
						continue
					}
					response, err = http.Get(endpoint("MAILPIT") + "/api/v1/message/" + message.ID)
					if err != nil {
						continue
					}
					var content struct {
						Text string `json:"Text"`
					}
					_ = json.NewDecoder(response.Body).Decode(&content)
					_ = response.Body.Close()
					for _, word := range strings.Fields(content.Text) {
						word = strings.Trim(word, ".,:;\r\n")
						if len(word) == 6 && strings.Trim(word, "0123456789") == "" {
							usedMessages[message.ID] = true
							return word
						}
					}
				}
			}
		}
		t.Fatal("verification mail code was not delivered to fixture inbox")
		return ""
	}
	for range 3 {
		code := mailCode(email, "verify")
		verification, fields = flow("verification", "flow="+verification.ID)
		fields.Set("method", "code")
		fields.Set("code", code)
		response, raw = submit("verification", verification, fields)
		if response.StatusCode != 303 {
			t.Fatalf("verify status=%d %s", response.StatusCode, raw)
		}
		status, err = service.call(ctx, "GET", cfg.KratosAdmin+"/admin/identities/"+ids[0].ID, nil, "", &ids[0])
		if err == nil && status == 200 && verified(ids[0]) {
			break
		}
	}
	if err != nil || status != 200 || !verified(ids[0]) {
		t.Fatal("email verification did not complete")
	}
	t.Log("self-registration, unverified identity and mail code verification passed")
	response, _ = request("GET", cfg.Site+"/auth/start", nil)
	next := response.Header.Get("Location")
	csrf := func() string {
		t.Helper()
		_, raw := request("GET", cfg.Site+"/auth/csrf", nil)
		var value map[string]string
		_ = json.Unmarshal(raw, &value)
		return value["csrf"]
	}
	broker := func(kind, challenge string) map[string]string {
		t.Helper()
		req, _ := http.NewRequest("POST", cfg.Site+"/auth/broker/"+kind+"?challenge="+url.QueryEscape(challenge), nil)
		req.Header.Set("Origin", cfg.Site)
		req.Header.Set("X-CSRF-Token", csrf())
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value map[string]string
		_ = json.NewDecoder(response.Body).Decode(&value)
		if response.StatusCode != 200 {
			t.Fatalf("broker %s status=%d %v", kind, response.StatusCode, value)
		}
		return value
	}
	authorize := func(next string) {
		completed := false
		for range 12 {
			target, err := url.Parse(next)
			if err != nil {
				t.Fatal(err)
			}
			if target.Path == "/auth/login" {
				challenge := target.Query().Get("login_challenge")
				value := broker("login", challenge)
				if value["required"] == "login" {
					login, fields := flow("login", "login_challenge="+url.QueryEscape(challenge))
					fields.Set("method", "password")
					fields.Set("identifier", email)
					fields.Set("password", password)
					response, _ = submit("login", login, fields)
					next = response.Header.Get("Location")
					continue
				}
				if value["required"] != "" {
					t.Fatalf("unexpected login requirement: %v", value)
				}
				next = value["redirect"]
				continue
			}
			if target.Path == "/auth/consent" {
				next = broker("consent", target.Query().Get("consent_challenge"))["redirect"]
				continue
			}
			response, raw = request("GET", next, nil)
			next = response.Header.Get("Location")
			if target.Path == "/auth/callback" {
				if response.StatusCode != 303 {
					t.Fatalf("callback status=%d %s", response.StatusCode, raw)
				}
				secureCookie := false
				for _, value := range response.Cookies() {
					if value.Name == "__Host-koujou-session" {
						secureCookie = value.Secure && value.HttpOnly && value.Path == "/" && value.SameSite == http.SameSiteLaxMode
					}
				}
				if !secureCookie {
					t.Fatal("BFF cookie security attributes missing")
				}
				completed = true
				break
			}
			if next == "" {
				t.Fatalf("authorization ended at %s status=%d body=%s", target.Path, response.StatusCode, raw)
			}
		}
		if !completed {
			t.Fatal("authorization code flow did not complete")
		}
	}
	authorize(next)
	var issuedEncrypted []byte
	if err = store.Database().QueryRow(ctx, "SELECT tokens FROM auth_sessions LIMIT 1").Scan(&issuedEncrypted); err != nil {
		t.Fatal(err)
	}
	issuedRaw, err := service.open(issuedEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	var issued Tokens
	if err = json.Unmarshal(issuedRaw, &issued); err != nil {
		t.Fatal(err)
	}
	freshInfo, err := service.Inspect(ctx, issued.Access)
	if err != nil {
		t.Fatal("fresh Ory access token rejected")
	}
	if wait := time.Until(time.Unix(freshInfo.Expires, 0).Add(time.Second)); wait > 10*time.Second {
		t.Fatal("fixture access-token lifetime exceeds test bound")
	} else if wait > 0 {
		time.Sleep(wait)
	}
	if _, err = service.Inspect(ctx, issued.Access); err == nil {
		t.Fatal("expired Ory-issued access token accepted")
	}
	response, raw = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 200 {
		t.Fatalf("me status=%d %s", response.StatusCode, raw)
	}
	var profile struct {
		User User `json:"user"`
	}
	_ = json.Unmarshal(raw, &profile)
	if profile.User.ID == "" || profile.User.Email != email || profile.User.Administrator {
		t.Fatal("profile or initial-administrator boundary failed")
	}
	found := false
	for _, value := range jar.Cookies(mustURL(t, cfg.Site)) {
		if value.Name == "__Host-koujou-session" {
			found = true
		}
	}
	if !found {
		t.Fatal("BFF session cookie missing")
	}
	tokens, err := service.requestTokens(&http.Request{Header: http.Header{"Cookie": {cookieHeader(jar.Cookies(mustURL(t, cfg.Site)))}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Inspect(ctx, tokens.ID); err == nil {
		t.Fatal("ID token accepted as access token")
	}
	if _, err = service.Inspect(ctx, tokens.Refresh); err == nil {
		t.Fatal("refresh token accepted as access token")
	}
	if bytes.Contains(raw, []byte(tokens.Access)) || bytes.Contains(raw, []byte(tokens.Refresh)) || bytes.Contains(raw, []byte(tokens.ID)) {
		t.Fatal("OAuth credential returned in profile response")
	}
	t.Log("real Ory code+PKCE, ID token nonce, BFF session, API audience/scope and ID/refresh-token rejection passed")
	// Browser-session updates require a same-origin CSRF token, including logout.
	response, _ = request("POST", cfg.Site+"/auth/logout", nil)
	if response.StatusCode != 403 {
		t.Fatal("invalid CSRF logout was accepted")
	}
	callback, _ := request("GET", cfg.Site+"/auth/callback?state=wrong&code=wrong", nil)
	if callback.StatusCode != 401 {
		t.Fatal("callback state mismatch accepted")
	}
	var encrypted []byte
	if err = store.Database().QueryRow(ctx, "SELECT tokens FROM auth_sessions LIMIT 1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(tokens.Access)) || bytes.Contains(encrypted, []byte(tokens.Refresh)) {
		t.Fatal("plaintext OAuth credentials stored in DB")
	}
	for _, pair := range [][2]string{{"issuer-one", "subject-one"}, {"issuer-two", "subject-two"}} {
		if _, err = service.mapUser(ctx, pair[0], pair[1], "same@koujou.test", "old-name"); err != nil {
			t.Fatal(err)
		}
	}
	firstUser, err := service.mapUser(ctx, "issuer-one", "subject-one", "same@koujou.test", "new-name")
	if err != nil {
		t.Fatal(err)
	}
	secondUser, err := service.mapUser(ctx, "issuer-two", "subject-two", "same@koujou.test", "other-name")
	if err != nil || firstUser.ID == secondUser.ID {
		t.Fatal("email merged different identity subjects")
	}
	var retainedID string
	if err = store.Database().QueryRow(ctx, "SELECT id FROM auth_users WHERE issuer='issuer-one' AND subject='subject-one'").Scan(&retainedID); err != nil || retainedID != firstUser.ID {
		t.Fatal("display name changed stable User ID")
	}
	t.Log("CSRF/state rejection, encrypted storage and issuer/sub mapping passed")
	// Rotate an actual Hydra refresh token through the BFF, then expire only the app session.
	var sessionID string
	for _, value := range jar.Cookies(mustURL(t, cfg.Site)) {
		if value.Name == "__Host-koujou-session" {
			sessionID = hash(value.Value)
		}
	}
	tokens.Expires = time.Now().Add(-time.Second)
	encoded, _ := json.Marshal(tokens)
	_, err = store.Database().Exec(ctx, "UPDATE auth_sessions SET tokens=$1 WHERE id=$2", service.seal(encoded), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	response, _ = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 200 {
		t.Fatal("BFF refresh failed")
	}
	tokens, err = service.requestTokens(&http.Request{Header: http.Header{"Cookie": {cookieHeader(jar.Cookies(mustURL(t, cfg.Site)))}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Database().Exec(ctx, "UPDATE auth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1", sessionID)
	response, _ = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 401 {
		t.Fatal("expired app session accepted")
	}
	if _, err = service.Inspect(ctx, tokens.Access); err != nil {
		t.Fatal("app expiry revoked separate OAuth token")
	}
	_, _ = store.Database().Exec(ctx, "UPDATE auth_sessions SET expires_at=now()+interval '1 hour' WHERE id=$1", sessionID)
	// Stop only services labelled with this isolated test project.
	for _, name := range []string{"hydra", "kratos"} {
		output, err := exec.Command("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+os.Getenv("KOUJOU_TEST_PROJECT"), "--filter", "label=com.docker.compose.service="+name).Output()
		if err != nil || strings.TrimSpace(string(output)) == "" {
			t.Fatal("test service lookup failed")
		}
		container := strings.TrimSpace(string(output))
		if output, err = exec.Command("docker", "stop", container).CombinedOutput(); err != nil {
			t.Fatalf("test stop failed: %s", output)
		}
		response, _ = request("GET", cfg.Site+"/auth/me", nil)
		if response.StatusCode != 503 {
			t.Fatalf("%s outage accepted authentication: %d", name, response.StatusCode)
		}
		if name == "hydra" {
			tokens.Expires = time.Now().Add(-time.Second)
			encoded, _ = json.Marshal(tokens)
			_, _ = store.Database().Exec(ctx, "UPDATE auth_sessions SET tokens=$1 WHERE id=$2", service.seal(encoded), sessionID)
			response, _ = request("GET", cfg.Site+"/auth/me", nil)
			if response.StatusCode != 503 {
				t.Fatal("refresh outage was not unavailable")
			}
		}
		if output, err = exec.Command("docker", "start", container).CombinedOutput(); err != nil {
			t.Fatalf("test restart failed: %s", output)
		}
		for start := time.Now(); time.Since(start) < 15*time.Second; time.Sleep(100 * time.Millisecond) {
			response, _ = request("GET", cfg.Site+"/auth/me", nil)
			if response.StatusCode == 200 {
				break
			}
		}
		if response.StatusCode != 200 {
			t.Fatal("authentication did not recover after provider restart")
		}
	}
	t.Log("BFF refresh, app expiry, separate OAuth validity, real Hydra/Kratos outage and refresh failure passed")
	// Revoke only the Kratos browser session; existing application credentials stay separate.
	status, err = service.call(ctx, "DELETE", cfg.KratosAdmin+"/admin/identities/"+ids[0].ID+"/sessions", nil, "", nil)
	if err != nil || status >= 400 {
		t.Fatal("Kratos session revocation failed")
	}
	response, _ = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 200 {
		t.Fatal("Kratos session revocation revoked separate app credentials")
	}
	recovery, fields := flow("recovery", "")
	fields.Set("method", "code")
	fields.Set("email", email)
	_, _ = submit("recovery", recovery, fields)
	recovery, fields = flow("recovery", "flow="+recovery.ID)
	fields.Set("method", "code")
	fields.Set("code", mailCode(email, "recover"))
	_, _ = submit("recovery", recovery, fields)
	recovery, _ = flow("recovery", "flow="+recovery.ID)
	if recovery.State != "passed_challenge" {
		t.Fatalf("recovery did not pass: %s messages=%s", recovery.State, recovery.UI.Messages)
	}
	passwordSetting, passwordFields := flow("settings", "")
	password = "Recovered-" + rand.Text() + "aA1!"
	passwordFields.Set("method", "password")
	passwordFields.Set("password", password)
	response, _ = submit("settings", passwordSetting, passwordFields)
	if response.StatusCode != 303 {
		t.Fatal("recovered password setting failed")
	}
	// Check the newly set password in a fresh browser session.
	client.Jar, _ = cookiejar.New(nil)
	loginAfterRecovery, loginFields := flow("login", "")
	loginFields.Set("method", "password")
	loginFields.Set("identifier", email)
	loginFields.Set("password", password)
	_, _ = submit("login", loginAfterRecovery, loginFields)
	var recoveredSession KratosSession
	status, err = service.call(ctx, "GET", cfg.KratosPublic+"/sessions/whoami", nil, cookieHeader(client.Jar.Cookies(mustURL(t, cfg.Site))), &recoveredSession)
	if err != nil || status != 200 || !recoveredSession.Active {
		t.Fatal("login with recovered password failed")
	}
	// Retain the app cookie for the later independent MFA/token assertion.
	for _, value := range jar.Cookies(mustURL(t, cfg.Site)) {
		if value.Name == "__Host-koujou-session" {
			client.Jar.SetCookies(mustURL(t, cfg.Site), []*http.Cookie{value})
		}
	}
	jar = client.Jar.(*cookiejar.Jar)
	t.Log("Kratos-only revocation, mail-code recovery, password setting and new-password login passed")
	settings, fields := flow("settings", "")
	secret := ""
	for _, rawNode := range settings.UI.Nodes {
		var node struct {
			Attributes struct {
				ID   string `json:"id"`
				Text struct {
					Text    string         `json:"text"`
					Context map[string]any `json:"context"`
				} `json:"text"`
			} `json:"attributes"`
		}
		_ = json.Unmarshal(rawNode, &node)
		if node.Attributes.ID == "totp_secret_key" {
			secret = node.Attributes.Text.Text
			if value, ok := node.Attributes.Text.Context["secret"].(string); ok {
				secret = value
			}
		}
	}
	secret = strings.ReplaceAll(strings.TrimSpace(secret), " ", "")
	if _, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret); err != nil || secret == "" {
		t.Fatal("TOTP enrollment secret not available in settings flow")
	}
	fields.Set("method", "totp")
	fields.Set("totp_code", totp(t, secret))
	response, raw = submit("settings", settings, fields)
	if response.StatusCode != 303 {
		t.Fatalf("TOTP setup status=%d", response.StatusCode)
	}
	var enrolled Identity
	status, err = service.call(ctx, "GET", cfg.KratosAdmin+"/admin/identities/"+ids[0].ID+"?include_credential=totp", nil, "", &enrolled)
	if err != nil || status != 200 {
		t.Fatal("TOTP credential lookup failed")
	}
	if _, ok := enrolled.Credentials["totp"]; !ok {
		t.Fatal("TOTP enrollment not persisted")
	}
	response, _ = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 401 {
		t.Fatal("old AAL1 token bypassed newly enrolled MFA")
	}
	logoutRequest, _ := http.NewRequest("POST", cfg.Site+"/auth/logout", nil)
	logoutRequest.Header.Set("Origin", cfg.Site)
	logoutRequest.Header.Set("X-CSRF-Token", csrf())
	logoutResponse, err := client.Do(logoutRequest)
	if err != nil {
		t.Fatal(err)
	}
	var ended map[string]string
	_ = json.NewDecoder(logoutResponse.Body).Decode(&ended)
	_ = logoutResponse.Body.Close()
	if ended["app_session"] != "ended" || ended["oauth_tokens"] != "revoked" || ended["kratos_session"] != "ended" {
		t.Fatalf("logout scopes: %v", ended)
	}
	if _, err = service.Inspect(ctx, tokens.Access); err == nil {
		t.Fatal("logout left current access token active")
	}
	passwordLogin, passwordFields := flow("login", "")
	passwordFields.Set("method", "password")
	passwordFields.Set("identifier", email)
	passwordFields.Set("password", password)
	_, _ = submit("login", passwordLogin, passwordFields)
	stepup, fields := flow("login", "aal=aal2")
	fields.Set("method", "totp")
	fields.Set("totp_code", totp(t, secret))
	response, _ = submit("login", stepup, fields)
	if response.StatusCode != 303 {
		t.Fatal("TOTP second factor failed")
	}
	var session KratosSession
	status, err = service.call(ctx, "GET", cfg.KratosPublic+"/sessions/whoami", nil, cookieHeader(jar.Cookies(mustURL(t, cfg.Site))), &session)
	if err != nil || status != 200 || session.AAL != "aal2" {
		t.Fatal("TOTP did not elevate Kratos session")
	}
	response, _ = request("GET", cfg.Site+"/auth/start", nil)
	authorize(response.Header.Get("Location"))
	response, _ = request("GET", cfg.Site+"/auth/me", nil)
	if response.StatusCode != 200 {
		t.Fatal("TOTP login did not return to authenticated app")
	}
	t.Log("optional TOTP enrollment, AAL1 API denial, second factor and authenticated app return passed")
}

func totp(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal("invalid fixture TOTP secret")
	}
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(value[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1000000)
}

func TestIntrospectionRejectsInvalidClaims(t *testing.T) {
	valid := tokenInfo{Active: true, Subject: "subject", Issuer: "https://issuer/", Audience: []string{"api"}, Scope: "koujou", Use: "access_token", Expires: time.Now().Add(time.Minute).Unix()}
	for _, test := range []struct {
		name  string
		alter func(*tokenInfo)
	}{{"expired", func(v *tokenInfo) { v.Expires = 1 }}, {"issuer", func(v *tokenInfo) { v.Issuer = "https://foreign/" }}, {"audience", func(v *tokenInfo) { v.Audience = []string{"foreign"} }}, {"scope", func(v *tokenInfo) { v.Scope = "openid" }}, {"ID token", func(v *tokenInfo) { v.Use = "id_token" }}, {"revoked", func(v *tokenInfo) { v.Active = false }}} {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.alter(&value)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(value) }))
			defer server.Close()
			service := &Service{Config: Config{Issuer: valid.Issuer, Audience: "api", HydraAdmin: server.URL}, HTTP: server.Client()}
			if _, err := service.Inspect(context.Background(), "synthetic-token"); err == nil {
				t.Fatal("invalid access token accepted")
			}
		})
	}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
func cookieHeader(cookies []*http.Cookie) string {
	values := []string{}
	for _, cookie := range cookies {
		values = append(values, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(values, "; ")
}
