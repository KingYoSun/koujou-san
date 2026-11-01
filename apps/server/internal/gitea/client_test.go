package gitea

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestExplicitInstanceAndCA(t *testing.T) {
	ctx := context.Background()
	newClient := func(server *httptest.Server, token string, trust bool) *Client {
		t.Helper()
		dir := t.TempDir()
		file := filepath.Join(dir, "token")
		if err := os.WriteFile(file, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		i := Instance{URL: server.URL, TokenFile: file}
		if trust {
			i.CAFile = filepath.Join(dir, "ca.pem")
			if err := os.WriteFile(i.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
		}
		c, err := i.Open()
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, token := range []string{"owned-first", "owned-second"} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "token "+token || r.URL.Path != "/api/v1/repos/proof/private" {
				t.Error("instance credentials or repository changed")
			}
			_, _ = w.Write([]byte(`{"id":1,"full_name":"proof/private"}`))
		}))
		c := newClient(server, token, true)
		if err := c.Repository(ctx, "proof/private", 1); err != nil {
			t.Fatal(err)
		}
		if err := c.Repository(ctx, "proof/private", 2); err == nil {
			t.Fatal("different repository ID accepted")
		}
		if err := newClient(server, token, false).Repository(ctx, "proof/private", 1); err == nil {
			t.Fatal("untrusted HTTPS accepted")
		}
		server.Close()
	}
	received := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received++ }))
	defer other.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if err := newClient(redirect, "owned-no-forward", true).Repository(ctx, "proof/private", 1); err == nil || received != 0 {
		t.Fatal("redirect forwarded instance authentication")
	}
	var requests atomic.Int32
	ready := make(chan struct{})
	limited := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 4 {
			close(ready)
		}
		<-ready
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer limited.Close()
	c := newClient(limited, "owned-limited", true)
	results := make(chan error, 4)
	for range 4 {
		go func() { results <- c.Repository(ctx, "proof/private", 1) }()
	}
	for range 4 {
		if err := <-results; err == nil || err.Error() != "rate_limited" {
			t.Fatal("missing rate limit", err)
		}
	}
	if err := c.Repository(ctx, "proof/private", 1); err == nil || requests.Load() != 4 {
		t.Fatal("Retry-After did not hold concurrent callers", err)
	}
	if Resource("existing-target") != "github:existing-target" || Resource("gitea:proof") != "gitea:proof" {
		t.Fatal("saved resource identity changed")
	}
}
