package chatgptplan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const Issuer = "https://auth.openai.com"
const Resource = "https://api.openai.com/v1"
const scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

type Provider struct {
	Issuer, Resource string
	HTTP             *http.Client
}

func Default() Provider { return Provider{Issuer: Issuer, Resource: Resource} }

func (p Provider) client() *http.Client {
	c := http.Client{Timeout: 30 * time.Second}
	if p.HTTP != nil {
		c = *p.HTTP
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

func (p Provider) verify(ctx context.Context, client, token, nonce, subject string) (string, string, error) {
	ctx = oidc.ClientContext(ctx, p.client())
	provider, err := oidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return "", "", ErrUnavailable
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: client}).Verify(ctx, token)
	if err != nil || verified.Subject == "" || (nonce != "" && verified.Nonce != nonce) || (subject != "" && subject != verified.Subject) {
		return "", "", ErrAuthentication
	}
	var claims struct {
		Email string `json:"email"`
	}
	if verified.Claims(&claims) != nil {
		return "", "", ErrAuthentication
	}
	return verified.Subject, claims.Email, nil
}

func (p Provider) exchange(ctx context.Context, c Credential, values url.Values, nonce string) (Credential, error) {
	values.Set("client_id", c.ClientID)
	values.Set("resource", p.Resource)
	req, _ := http.NewRequestWithContext(ctx, "POST", p.Issuer+"/api/accounts/oauth/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client().Do(req)
	if err != nil {
		return c, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return c, ErrAuthentication
	}
	var token struct {
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token) != nil {
		return c, ErrAuthentication
	}
	if token.IDToken != "" {
		c.Subject, c.Email, err = p.verify(ctx, c.ClientID, token.IDToken, nonce, c.Subject)
		if err != nil {
			return c, err
		}
		c.IDToken = token.IDToken
	} else if nonce != "" {
		return c, ErrAuthentication
	}
	if token.Scope != "" {
		c.Scopes = strings.Fields(token.Scope)
	} else if nonce != "" {
		return c, ErrAuthentication
	}
	c.AccessToken, c.RefreshToken = token.AccessToken, token.RefreshToken
	c.TokenType, c.ExpiresIn, c.SavedAt = token.TokenType, token.ExpiresIn, time.Now().UTC()
	if !c.ready(p.Issuer) {
		return c, ErrAuthentication
	}
	return c, nil
}

// Access refreshes only an expired access token, under a cross-process file lock.
// It never opens an interactive login or retries an inference request.
func (p Provider) Access(ctx context.Context, file string) (Credential, error) {
	unlock, err := lock(ctx, file)
	if err != nil {
		return Credential{}, err
	}
	defer unlock()
	c, err := Read(file)
	if err != nil || !c.ready(p.Issuer) {
		return Credential{}, ErrAuthentication
	}
	if time.Now().Before(c.SavedAt.Add(time.Duration(c.ExpiresIn) * time.Second)) {
		return c, nil
	}
	c, err = p.exchange(ctx, c, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}}, "")
	if err == nil {
		err = Save(file, c)
	}
	return c, err
}

func newHost() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&15|64, b[8]&63|128
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Login is an operator action. The callback is always loopback and is separate
// from the public application login and from active model requests.
func (p Provider) Login(ctx context.Context, file string, port int, prompt func(string) error) error {
	unlock, err := lock(ctx, file)
	if err != nil {
		return err
	}
	c, err := Read(file)
	if _, exists := os.Stat(file); os.IsNotExist(exists) {
		c, err = Credential{Issuer: p.Issuer, HostID: newHost()}, nil
		if err = Save(file, c); err != nil {
			unlock()
			return err
		}
	}
	unlock()
	if err != nil || c.Issuer != p.Issuer || c.HostID == "" {
		return ErrAuthentication
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return ErrUnavailable
	}
	callback := "http://" + listener.Addr().String() + "/auth/callback"
	state, nonce, verifier := rand.Text(), rand.Text(), rand.Text()+rand.Text()
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {c.ClientID}, "ext_agent_host_id": {c.HostID}, "response_type": {"code"}, "redirect_uri": {callback}, "scope": {scopes}, "resource": {p.Resource}, "state": {state}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	if c.ClientID == "" {
		query.Set("client_id", "dynamic_agent_client")
		query.Set("agent_name_hint", "koujou-san")
	}
	type result struct{ code, client, denied string }
	returned := make(chan result, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Authorization state does not match.", 400)
			return
		}
		once.Do(func() {
			returned <- result{r.URL.Query().Get("code"), r.URL.Query().Get("client_id"), r.URL.Query().Get("error")}
		})
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "Authorization received. Return to the terminal to check the result.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	if err = prompt(p.Issuer + "/api/accounts/authorize?" + query.Encode()); err != nil {
		return err
	}
	var done result
	select {
	case <-ctx.Done():
		return ctx.Err()
	case done = <-returned:
	}
	if done.denied != "" || done.code == "" || (c.ClientID != "" && done.client != "" && done.client != c.ClientID) {
		return ErrAuthentication
	}
	if c.ClientID == "" {
		c.ClientID = done.client
	}
	if c.ClientID == "" || c.ClientID == "dynamic_agent_client" {
		return ErrAuthentication
	}
	unlock, err = lock(ctx, file)
	if err != nil {
		return err
	}
	defer unlock()
	latest, err := Read(file)
	if err != nil || latest.HostID != c.HostID || (latest.ClientID != "" && latest.ClientID != c.ClientID) || latest.Subject != c.Subject {
		return ErrAuthentication
	}
	if latest.AccessToken == "" {
		latest.ClientID = c.ClientID
		if err = Save(file, latest); err != nil {
			return err
		}
	}
	c, err = p.exchange(ctx, c, url.Values{"grant_type": {"authorization_code"}, "code": {done.code}, "code_verifier": {verifier}, "redirect_uri": {callback}}, nonce)
	if err != nil {
		return err
	}
	return Save(file, c)
}

func (p Provider) Logout(ctx context.Context, file string) (bool, error) {
	unlock, err := lock(ctx, file)
	if err != nil {
		return false, err
	}
	defer unlock()
	c, err := Read(file)
	if err != nil || c.Issuer != p.Issuer {
		return false, ErrAuthentication
	}
	confirmed := c.RefreshToken == ""
	if c.RefreshToken != "" {
		provider, discoveryErr := oidc.NewProvider(oidc.ClientContext(ctx, p.client()), p.Issuer)
		var metadata struct {
			Revocation string `json:"revocation_endpoint"`
		}
		if discoveryErr == nil && provider.Claims(&metadata) == nil {
			u, _ := url.Parse(metadata.Revocation)
			issuer, _ := url.Parse(p.Issuer)
			if u != nil && u.Scheme == issuer.Scheme && u.Host == issuer.Host && u.User == nil {
				body := url.Values{"token": {c.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {c.ClientID}}
				r, _ := http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(body.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if response, callErr := p.client().Do(r); callErr == nil {
					confirmed = response.StatusCode == 200
					response.Body.Close()
				}
			}
		}
	}
	c.AccessToken, c.RefreshToken, c.IDToken = "", "", ""
	c.Scopes, c.ExpiresIn, c.SavedAt = nil, 0, time.Time{}
	return confirmed, Save(file, c)
}
