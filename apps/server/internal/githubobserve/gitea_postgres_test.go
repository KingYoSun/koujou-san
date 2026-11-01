package githubobserve

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkGiteaPostgres(t *testing.T, ctx context.Context, store *persistence.Store, a *access.Service, original *Config) {
	t.Helper()
	c := *original
	c.Targets = append([]Target{}, original.Targets...)
	c.Bindings = append([]Binding{}, original.Bindings...)
	var legacy []byte
	if err := store.Database().QueryRow(ctx, "SELECT snapshot FROM github_targets WHERE id='action'").Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		key := "owned-" + name
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.Header.Get("Authorization") != "token "+key {
				t.Error("Gitea observation wrote or mixed credentials")
			}
			switch r.URL.Path {
			case "/api/v1/repos/proof/private":
				_, _ = w.Write([]byte(`{"id":1,"full_name":"proof/private"}`))
			case "/api/v1/version":
				_, _ = w.Write([]byte(`{"version":"28.2.0"}`))
			case "/api/v1/repos/proof/private/issues/1":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "number": 1, "html_url": server.URL + "/proof/private/issues/1", "title": name, "state": "open"})
			default:
				w.WriteHeader(404)
			}
		}))
		t.Cleanup(server.Close)
		dir := t.TempDir()
		token, ca, secret := filepath.Join(dir, "token"), filepath.Join(dir, "ca"), filepath.Join(dir, "secret")
		_ = os.WriteFile(token, []byte(key), 0600)
		_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
		_ = os.WriteFile(secret, []byte("hook-"+name), 0600)
		c.GiteaInstances = append(c.GiteaInstances, gitea.Instance{URL: server.URL, TokenFile: token, CAFile: ca, WebhookSecretFile: secret})
		c.Targets = append(c.Targets, Target{ID: "gitea:" + name, Provider: "gitea", Instance: server.URL, Repository: "proof/private", RepositoryID: 1, Kind: "issue", ExternalID: "1"})
		c.Bindings = append(c.Bindings, Binding{WorkspaceID: "one", TargetID: "gitea:" + name})
	}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, store, a, &c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Database().Exec(ctx, "UPDATE github_targets SET due_at=now()+interval '1 day' WHERE COALESCE(spec->>'provider','')=''"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if worked, err := s.step(ctx); err != nil || !worked {
			t.Fatal("Gitea worker", worked, err)
		}
	}
	for _, target := range c.Targets {
		if target.Provider != "gitea" {
			continue
		}
		var value []byte
		if err := store.Database().QueryRow(ctx, "SELECT snapshot FROM github_targets WHERE id=$1", target.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		var snapshot Snapshot
		_ = json.Unmarshal(value, &snapshot)
		if snapshot.Instance != target.Instance || snapshot.RepositoryID != 1 || snapshot.Title != strings.TrimPrefix(target.ID, "gitea:") {
			t.Fatal("same-name Gitea targets mixed")
		}
	}
	var after []byte
	_ = store.Database().QueryRow(ctx, "SELECT snapshot FROM github_targets WHERE id='action'").Scan(&after)
	if string(after) != string(legacy) {
		t.Fatal("saved GitHub observation changed")
	}
	if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: "gitea:first", Restricted: true}, []access.ResourceGrant{{UserID: "alice", Read: true}})
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	get := func(path, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Fixture-User", user)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	for _, item := range []struct {
		path, user string
		want       int
	}{{"/forge/workspaces/one/observations", "alice", 6}, {"/forge/workspaces/one/observations", "bob", 5}, {"/github/workspaces/one/observations", "alice", 4}} {
		w := get(item.path, item.user)
		var response apicontract.Response[[]View]
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		if w.Code != 200 || response.Data == nil || len(*response.Data) != item.want {
			t.Fatalf("current scope/legacy list %s: %d", item.path, w.Code)
		}
	}
	if get("/forge/workspaces/one/observations/gitea:first/history", "bob").Code != 403 {
		t.Fatal("Gitea history leaked")
	}
	_, _ = store.Database().Exec(ctx, "UPDATE github_targets SET due_at=now()+interval '1 day' WHERE spec->>'provider'='gitea'")
	var first *gitea.Client
	for _, client := range c.clients {
		if strings.Contains(client.URL, c.Targets[len(c.Targets)-2].Instance) {
			first = client
		}
	}
	body := []byte(`{"repository":{"id":1},"issue":{"state":"closed","title":"untrusted payload"}}`)
	mac := hmac.New(sha256.New, first.Secret)
	_, _ = mac.Write(body)
	for range 2 {
		req := httptest.NewRequest("POST", "/gitea/webhook/"+first.WebhookID(), strings.NewReader(string(body)))
		req.Header.Set("X-Gitea-Signature", hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 202 {
			t.Fatal("Gitea notification", w.Code)
		}
	}
	var due int
	if err := store.Database().QueryRow(ctx, "SELECT count(*) FROM github_targets WHERE spec->>'provider'='gitea' AND due_at<=now()").Scan(&due); err != nil || due != 1 {
		t.Fatal("notification crossed instances", due, err)
	}
	if _, err := s.step(ctx); err != nil {
		t.Fatal(err)
	}
	var current []byte
	_ = store.Database().QueryRow(ctx, "SELECT snapshot FROM github_targets WHERE id='gitea:first'").Scan(&current)
	if strings.Contains(string(current), "untrusted payload") || !strings.Contains(string(current), `"state": "open"`) {
		t.Fatal("webhook payload became authoritative")
	}
	changed := c
	changed.Targets = append([]Target{}, c.Targets...)
	changed.Targets[len(changed.Targets)-2].RepositoryID = 2
	if _, err := New(ctx, store, a, &changed); err == nil {
		t.Fatal("Gitea identity changed under saved target")
	}
	t.Log("Gitea instance isolation, current read scope, signed reconciliation and saved GitHub references passed")
}
