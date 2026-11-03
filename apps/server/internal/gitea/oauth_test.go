package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestOAuthExchangeDoesNotFollowRedirectsOrReturnErrorSecrets(t *testing.T) {
	var foreign atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreign.Add(1) }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.URL.Path != "/login/oauth/access_token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Form.Get("client_secret") != "owned-secret" {
			t.Error("wrong token exchange")
		}
		http.Redirect(w, r, other.URL, 307)
	}))
	defer origin.Close()
	if _, err := Exchange(context.Background(), origin.URL, "", url.Values{"client_secret": {"owned-secret"}}); err != ErrConfig {
		t.Fatal("redirect accepted")
	}
	if foreign.Load() != 0 {
		t.Fatal("secret redirected to foreign origin")
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "owned-secret", 401) }))
	defer failure.Close()
	if _, err := Exchange(context.Background(), failure.URL, "", url.Values{}); err != ErrConfig {
		t.Fatal("remote error returned")
	}
}

func TestOAuthBearerAndTokenEnvelope(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			json.NewEncoder(w).Encode(OAuthToken{Access: "owned-access", Refresh: "owned-refresh", Type: "bearer", Expires: 3600})
			return
		}
		if r.Header.Get("Authorization") != "Bearer owned-access" {
			t.Error("OAuth used PAT authentication")
		}
		json.NewEncoder(w).Encode(map[string]int{"id": 1})
	}))
	defer origin.Close()
	token, err := Exchange(context.Background(), origin.URL, "", url.Values{})
	if err != nil || token.Refresh != "owned-refresh" {
		t.Fatal(err)
	}
	client, err := FromOAuthCredentials(origin.URL, token.Access, "")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err = client.Get(context.Background(), "/user", &value); err != nil {
		t.Fatal(err)
	}
}
