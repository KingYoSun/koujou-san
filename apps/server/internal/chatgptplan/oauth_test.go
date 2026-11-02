package chatgptplan

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnedRegistrationRefreshAndRevocation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var nonce, challenge, redirect string
	var fieldsMu sync.Mutex
	var refreshed atomic.Int32
	var server *httptest.Server
	encode := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	sign := func(audience, tokenNonce string) string {
		value := encode(map[string]any{"alg": "RS256", "kid": "owned"}) + "." + encode(map[string]any{"iss": server.URL, "sub": "owned-subject", "aud": audience, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": tokenNonce, "email": "owner@example.test"})
		sum := sha256.Sum256([]byte(value))
		signature, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		return value + "." + base64.RawURLEncoding.EncodeToString(signature)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "jwks_uri": server.URL + "/keys", "revocation_endpoint": server.URL + "/revoke", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "owned", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/api/accounts/oauth/token":
			fieldsMu.Lock()
			currentNonce, currentChallenge, currentRedirect := nonce, challenge, redirect
			fieldsMu.Unlock()
			r.ParseForm()
			if r.Form.Get("client_id") != "owned-issued-client" || r.Form.Get("resource") != server.URL+"/v1" || r.Form.Get("client_secret") != "" {
				t.Error("registration binding changed")
			}
			refresh := r.Form.Get("grant_type") == "refresh_token"
			if refresh {
				if r.Form.Get("refresh_token") != "refresh-original" {
					t.Error("rotating refresh was replayed")
				}
				refreshed.Add(1)
			} else {
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(sum[:]) != currentChallenge || r.Form.Get("redirect_uri") != currentRedirect {
					t.Error("PKCE or exact callback lost")
				}
			}
			refreshToken := "refresh-original"
			if refresh {
				refreshToken = "refresh-rotated"
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "access-owned", "refresh_token": refreshToken, "id_token": sign("owned-issued-client", currentNonce), "token_type": "Bearer", "expires_in": 3600, "scope": scopes})
		case "/revoke":
			r.ParseForm()
			if r.Form.Get("token") != "refresh-rotated" || r.Form.Get("client_id") != "owned-issued-client" {
				t.Error("wrong revocation")
			}
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected provider request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := Provider{Issuer: server.URL, Resource: server.URL + "/v1", HTTP: server.Client()}
	file := filepath.Join(t.TempDir(), "credential.json")
	prompt := func(address string) error {
		u, _ := url.Parse(address)
		q := u.Query()
		fieldsMu.Lock()
		nonce, challenge, redirect = q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri")
		fieldsMu.Unlock()
		if q.Get("client_id") != "dynamic_agent_client" || q.Get("agent_name_hint") != "koujou-san" || !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") {
			t.Error("missing first-registration metadata")
		}
		callback, _ := url.Parse(redirect)
		bad := url.Values{"state": {"wrong"}, "code": {"owned"}, "client_id": {"owned-issued-client"}}
		res, err := http.Get(callback.String() + "?" + bad.Encode())
		if err != nil {
			return err
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Error("invalid callback state accepted")
		}
		bad.Set("state", q.Get("state"))
		res, err = http.Get(callback.String() + "?" + bad.Encode())
		if err != nil {
			return err
		}
		res.Body.Close()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = p.Login(ctx, file, 0, prompt); err != nil {
		t.Fatal(err)
	}
	c, err := Read(file)
	if err != nil || !c.ready(server.URL) || c.Subject != "owned-subject" || c.ClientID == "dynamic_agent_client" {
		t.Fatalf("registration not validated: %v", err)
	}
	host := c.HostID
	for _, invalid := range []struct{ name, token, nonce, subject string }{
		{"nonce", c.IDToken, "different-nonce", c.Subject},
		{"audience", sign("other-client", nonce), nonce, c.Subject},
		{"subject", c.IDToken, nonce, "different-subject"},
		{"signature", c.IDToken + "xx", nonce, c.Subject},
	} {
		t.Run("reject-"+invalid.name, func(t *testing.T) {
			if _, _, err := p.verify(ctx, c.ClientID, invalid.token, invalid.nonce, invalid.subject); err == nil {
				t.Fatal("invalid token accepted")
			}
		})
	}
	c.SavedAt = time.Now().Add(-2 * time.Hour)
	if err = Save(file, c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if credential, e := p.Access(ctx, file); e != nil || credential.RefreshToken != "refresh-rotated" {
				t.Errorf("refresh failed: %v", e)
			}
		})
	}
	wg.Wait()
	if refreshed.Load() != 1 {
		t.Fatalf("refresh calls=%d", refreshed.Load())
	}
	confirmed, err := p.Logout(ctx, file)
	if err != nil || !confirmed {
		t.Fatalf("revocation=%v %v", confirmed, err)
	}
	c, err = Read(file)
	if err != nil || c.HostID != host || c.ClientID != "owned-issued-client" || c.AccessToken != "" || c.IDToken != "" || c.RefreshToken != "" {
		t.Fatal("sign-out lost registration or retained tokens")
	}
	if _, err = p.Access(ctx, file); err == nil {
		t.Fatal("signed-out account used for inference")
	}
}

func TestCredentialWithoutPlanScopeCannotBeUsed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credential.json")
	c := Credential{Issuer: Issuer, Subject: "owned", ClientID: "issued", HostID: "urn:uuid:owned", IDToken: "owned", AccessToken: "owned", RefreshToken: "owned", TokenType: "Bearer", ExpiresIn: 3600, SavedAt: time.Now(), Scopes: []string{"openid", "offline_access", "resource.invoke"}}
	if err := Save(file, c); err != nil {
		t.Fatal(err)
	}
	if _, err := Default().Access(context.Background(), file); err == nil {
		t.Fatal("identity-only scope accepted")
	}
}
