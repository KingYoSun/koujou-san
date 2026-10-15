package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Site, API, Issuer, HydraAdmin, KratosPublic, KratosAdmin     string
	WebClient, WebSecret, NativeClient, NativeRedirect, Audience string
	Key                                                          []byte
}

type Service struct {
	Config   Config
	DB       *pgxpool.Pool
	HTTP     *http.Client
	verifier *oidc.IDTokenVerifier
	crypt    cipher.AEAD
}
type User struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	DisplayName   string `json:"display_name"`
	Administrator bool   `json:"administrator"`
}
type Identity struct {
	ID          string                     `json:"id"`
	State       string                     `json:"state"`
	Credentials map[string]json.RawMessage `json:"credentials"`
	Traits      struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"traits"`
	Verifiable []struct {
		Verified bool   `json:"verified"`
		Value    string `json:"value"`
	} `json:"verifiable_addresses"`
}
type KratosSession struct {
	Active   bool     `json:"active"`
	AAL      string   `json:"authenticator_assurance_level"`
	Identity Identity `json:"identity"`
}
type Tokens struct {
	Access    string    `json:"access_token"`
	Refresh   string    `json:"refresh_token"`
	ID        string    `json:"id_token"`
	ExpiresIn int64     `json:"expires_in"`
	Expires   time.Time `json:"expires"`
}
type tokenInfo struct {
	Active   bool     `json:"active"`
	Subject  string   `json:"sub"`
	Issuer   string   `json:"iss"`
	Audience []string `json:"aud"`
	Scope    string   `json:"scope"`
	Use      string   `json:"token_use"`
	Expires  int64    `json:"exp"`
	Extra    struct {
		AAL string `json:"aal"`
	} `json:"ext"`
}

var ErrUnauthorized = errors.New("authentication_required")

func New(ctx context.Context, config Config, db *pgxpool.Pool) (*Service, error) {
	config.Site = strings.TrimRight(config.Site, "/")
	config.API = strings.TrimRight(config.API, "/")
	config.Issuer = strings.TrimRight(config.Issuer, "/") + "/"
	site, err := url.Parse(config.Site)
	if err != nil || site.Scheme != "https" || site.Host == "" {
		return nil, errors.New("HTTPS site URL required")
	}
	block, err := aes.NewCipher(config.Key)
	if err != nil {
		return nil, errors.New("invalid authentication encryption key")
	}
	crypt, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery unavailable")
	}
	return &Service{Config: config, DB: db, HTTP: client, verifier: provider.Verifier(&oidc.Config{ClientID: config.WebClient}), crypt: crypt}, nil
}

func (s *Service) seal(data []byte) []byte {
	nonce := make([]byte, s.crypt.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return s.crypt.Seal(nonce, nonce, data, nil)
}
func (s *Service) open(data []byte) ([]byte, error) {
	n := s.crypt.NonceSize()
	if len(data) < n {
		return nil, ErrUnauthorized
	}
	return s.crypt.Open(nil, data[:n], data[n:], nil)
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) call(ctx context.Context, method, target string, body any, cookie string, result any) (int, error) {
	var raw io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		raw = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, raw)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := s.HTTP.Do(request)
	if err != nil {
		return 0, errors.New("identity_provider_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 {
		return response.StatusCode, errors.New("identity_provider_unavailable")
	}
	if result != nil && response.StatusCode < 400 {
		if err = json.NewDecoder(response.Body).Decode(result); err != nil {
			return response.StatusCode, errors.New("invalid_identity_response")
		}
	}
	return response.StatusCode, nil
}

func (s *Service) exchange(ctx context.Context, form url.Values, web bool) (Tokens, error) {
	var tokens Tokens
	request, err := http.NewRequestWithContext(ctx, "POST", s.Config.Issuer+"oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokens, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if web {
		request.SetBasicAuth(s.Config.WebClient, s.Config.WebSecret)
	}
	response, err := s.HTTP.Do(request)
	if err != nil {
		return tokens, errors.New("identity_provider_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 {
		return tokens, errors.New("identity_provider_unavailable")
	}
	if response.StatusCode != 200 {
		return tokens, ErrUnauthorized
	}
	if err = json.NewDecoder(response.Body).Decode(&tokens); err != nil {
		return tokens, errors.New("invalid_token_response")
	}
	tokens.Expires = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	return tokens, nil
}

func (s *Service) Inspect(ctx context.Context, token string) (tokenInfo, error) {
	return s.inspect(ctx, token, s.Config.Audience, "koujou")
}

func (s *Service) inspect(ctx context.Context, token, resource, requiredScope string) (tokenInfo, error) {
	var info tokenInfo
	request, err := http.NewRequestWithContext(ctx, "POST", s.Config.HydraAdmin+"/admin/oauth2/introspect", strings.NewReader(url.Values{"token": {token}, "token_type_hint": {"access_token"}}.Encode()))
	if err != nil {
		return info, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.HTTP.Do(request)
	if err != nil {
		return info, errors.New("identity_provider_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&info) != nil {
		return info, errors.New("invalid_introspection_response")
	}
	audience := false
	for _, value := range info.Audience {
		if value == resource {
			audience = true
		}
	}
	scope := false
	for _, value := range strings.Fields(info.Scope) {
		if value == requiredScope {
			scope = true
		}
	}
	if !info.Active || info.Subject == "" || info.Issuer != s.Config.Issuer || info.Use != "access_token" || info.Expires <= time.Now().Unix() || !audience || !scope {
		return info, ErrUnauthorized
	}
	return info, nil
}

func (s *Service) mapUser(ctx context.Context, issuer, subject, email, name string) (User, error) {
	var user User
	err := s.DB.QueryRow(ctx, `INSERT INTO auth_users(id,issuer,subject,email,display_name) VALUES($1,$2,$3,$4,$5) ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,display_name=EXCLUDED.display_name RETURNING id,email,display_name,administrator`, rand.Text(), issuer, subject, email, name).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Administrator)
	return user, err
}

func (s *Service) User(ctx context.Context, token string) (User, error) {
	info, err := s.Inspect(ctx, token)
	return s.identityUser(ctx, info, err)
}

// MCPUser validates a resource-bound access token and the same live identity
// state, verified email and MFA level as the ordinary API.
func (s *Service) MCPUser(ctx context.Context, token string) (User, time.Time, error) {
	info, err := s.inspect(ctx, token, s.Config.MCPResource(), "koujou.mcp")
	user, err := s.identityUser(ctx, info, err)
	return user, time.Unix(info.Expires, 0), err
}

func (s *Service) identityUser(ctx context.Context, info tokenInfo, err error) (User, error) {
	if err != nil {
		return User{}, err
	}
	var identity Identity
	status, err := s.call(ctx, "GET", s.Config.KratosAdmin+"/admin/identities/"+url.PathEscape(info.Subject)+"?include_credential=totp", nil, "", &identity)
	if err != nil {
		return User{}, err
	}
	if status != 200 || identity.State != "active" || !verified(identity) {
		return User{}, ErrUnauthorized
	}
	if _, enrolled := identity.Credentials["totp"]; enrolled && info.Extra.AAL != "aal2" {
		return User{}, ErrUnauthorized
	}
	name := identity.Traits.Name
	if name == "" {
		name = identity.Traits.Email
	}
	return s.mapUser(ctx, s.Config.Issuer, info.Subject, identity.Traits.Email, name)
}

func verified(identity Identity) bool {
	for _, address := range identity.Verifiable {
		if address.Verified && address.Value == identity.Traits.Email {
			return true
		}
	}
	return false
}

func (s *Service) Revoke(ctx context.Context, token string, web bool) error {
	form := url.Values{"token": {token}}
	if !web {
		form.Set("client_id", s.Config.NativeClient)
	}
	request, err := http.NewRequestWithContext(ctx, "POST", s.Config.Issuer+"oauth2/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if web {
		request.SetBasicAuth(s.Config.WebClient, s.Config.WebSecret)
	}
	response, err := s.HTTP.Do(request)
	if err != nil {
		return errors.New("identity_provider_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("revocation_unconfirmed")
	}
	return nil
}

func (s *Service) Bootstrap(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return errors.New("explicit administrator email and password are required")
	}
	if err := s.ProvisionClients(ctx); err != nil {
		return err
	}
	return s.bootstrapAdministrator(ctx, email, password)
}

// ProvisionClients registers bundled clients without changing any identity.
func (s *Service) ProvisionClients(ctx context.Context) error {
	for _, client := range []map[string]any{
		{"client_id": s.Config.WebClient, "client_secret": s.Config.WebSecret, "token_endpoint_auth_method": "client_secret_basic", "redirect_uris": []string{s.Config.Site + "/auth/callback"}},
		{"client_id": s.Config.NativeClient, "token_endpoint_auth_method": "none", "redirect_uris": []string{s.Config.NativeRedirect}},
	} {
		client["grant_types"] = []string{"authorization_code", "refresh_token"}
		client["response_types"] = []string{"code"}
		client["scope"] = "openid offline_access email profile koujou koujou.mcp"
		client["audience"] = []string{s.Config.Audience, s.Config.MCPResource()}
		id := client["client_id"].(string)
		var existing map[string]any
		status, err := s.call(ctx, "GET", s.Config.HydraAdmin+"/admin/clients/"+id, nil, "", &existing)
		if err != nil {
			return err
		}
		method, target := "POST", s.Config.HydraAdmin+"/admin/clients"
		if status == 200 {
			method = "PUT"
			target += "/" + id
		} else if status != 404 {
			return errors.New("OAuth client lookup failed")
		}
		status, err = s.call(ctx, method, target, client, "", nil)
		if err != nil || status >= 400 {
			return errors.New("OAuth client setup failed")
		}
	}
	return nil
}

func (s *Service) bootstrapAdministrator(ctx context.Context, email, password string) error {
	var identities []Identity
	status, err := s.call(ctx, "GET", s.Config.KratosAdmin+"/admin/identities?credentials_identifier="+url.QueryEscape(email), nil, "", &identities)
	if err != nil || status != 200 {
		return errors.New("identity lookup failed")
	}
	var identity Identity
	if len(identities) == 1 {
		identity = identities[0]
		if identity.Traits.Email != email {
			return errors.New("administrator identity does not match")
		}
	} else if len(identities) == 0 {
		status, err = s.call(ctx, "POST", s.Config.KratosAdmin+"/admin/identities", map[string]any{"schema_id": "default", "traits": map[string]string{"email": email}, "credentials": map[string]any{"password": map[string]any{"config": map[string]string{"password": password}}}}, "", &identity)
		if err != nil || status != 201 {
			return errors.New("explicit administrator creation failed")
		}
	} else {
		return errors.New("ambiguous administrator identity")
	}
	user, err := s.mapUser(ctx, s.Config.Issuer, identity.ID, email, email)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, "UPDATE auth_users SET administrator=true WHERE id=$1", user.ID)
	return err
}

func challenge(verifier string) string {
	value := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(value[:])
}
