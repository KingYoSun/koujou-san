package repositories

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/sharedsync"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

func TestWorkspaceSetupPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:forge")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, user := range []string{"setup-admin", "setup-manager", "setup-editor", "setup-outsider"} {
		if _, err = store.Database().Exec(ctx, `INSERT INTO auth_users VALUES($1,'setup-fixture',$1,$1||'@owned.test',$1,$2)`, user, user == "setup-admin"); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("s", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		user := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(user, "setup-") {
			http.Error(w, "owned authentication required", 401)
			return "", false
		}
		return user, true
	})
	s := New(store, a, []byte(strings.Repeat("s", 32)))
	mux := http.NewServeMux()
	a.Register(mux)
	s.Register(mux)
	cs := contexts.New(store, a, contexts.DefaultLimits())
	cs.Register(mux)
	sharedsync.New(store, a, cs, []byte(strings.Repeat("s", 32))).Register(mux)
	clientops.New(store, a, cs, usage.New(store, a)).Register(mux)
	gateway := serverllm.New(store)
	gateway.Register(mux, a)
	mux.HandleFunc("GET /hopper/workspaces/{workspace}/model-connections", func(w http.ResponseWriter, r *http.Request) {
		user, ok := a.User(w, r)
		if !ok {
			return
		}
		if a.Check(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), "", "read") != nil {
			http.Error(w, "forbidden", 403)
			return
		}
		values, err := gateway.Connections(r.PathValue("workspace"))
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		json.NewEncoder(w).Encode(values)
	})
	api := httptest.NewServer(mux)
	defer api.Close()
	s.Site = api.URL
	request := func(actor, method, path string, body any, status int) []byte {
		t.Helper()
		var input io.Reader
		if body != nil {
			input = bytes.NewReader(raw(body))
		}
		req, _ := http.NewRequest(method, api.URL+path, input)
		req.Header.Set("Authorization", "Bearer "+actor)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		value, _ := io.ReadAll(res.Body)
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, res.StatusCode, status, value)
		}
		for _, secret := range []string{"owned-client-secret", "owned-access-token", "owned-refresh-token"} {
			if bytes.Contains(value, []byte(secret)) {
				t.Fatal("secret reached HTTP response")
			}
		}
		return value
	}
	serial := 0
	mutation := func(version string) map[string]any {
		serial++
		return map[string]any{"expected_version": version, "operation_id": fmt.Sprintf("setup-%d", serial)}
	}
	for _, w := range []string{"setup", "setup-ui"} {
		body := mutation("0")
		body["name"] = w
		request("setup-admin", "POST", "/access/workspaces/"+w+"/create", body, 200)
	}
	if _, err = store.Database().Exec(ctx, `INSERT INTO access_members(workspace_id,user_id,role) VALUES('setup','setup-manager','admin'),('setup','setup-editor','editor')`); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	challenges := map[string]string{}
	var exchanges, refreshes, creates atomic.Int64
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/authorize" {
			q := r.URL.Query()
			if q.Get("client_id") != "owned-client" || q.Get("code_challenge_method") != "S256" || q.Get("scope") != "read:user write:repository write:issue" {
				http.Error(w, "bad authorization", 400)
				return
			}
			code := nonce()
			mu.Lock()
			challenges[code] = q.Get("code_challenge")
			mu.Unlock()
			callback, _ := url.Parse(q.Get("redirect_uri"))
			cq := callback.Query()
			cq.Set("code", code)
			cq.Set("state", q.Get("state"))
			callback.RawQuery = cq.Encode()
			http.Redirect(w, r, callback.String(), 302)
			return
		}
		if r.URL.Path == "/login/oauth/access_token" {
			r.ParseForm()
			if r.Form.Get("client_id") != "owned-client" || r.Form.Get("client_secret") != "owned-client-secret" {
				http.Error(w, "bad client", 401)
				return
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				mu.Lock()
				expected := challenges[r.Form.Get("code")]
				delete(challenges, r.Form.Get("code"))
				mu.Unlock()
				if expected == "" || expected != base64.RawURLEncoding.EncodeToString(challenge[:]) {
					http.Error(w, "bad proof", 400)
					return
				}
				exchanges.Add(1)
			case "refresh_token":
				if r.Form.Get("refresh_token") != "owned-refresh-token" {
					http.Error(w, "bad refresh", 400)
					return
				}
				refreshes.Add(1)
				json.NewEncoder(w).Encode(map[string]any{"access_token": "owned-access-token-rotated", "refresh_token": "owned-refresh-token-rotated", "token_type": "Bearer", "expires_in": 3600})
				return
			default:
				http.Error(w, "bad grant", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "owned-access-token", "refresh_token": "owned-refresh-token", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Header.Get("Authorization") != "Bearer owned-access-token" && r.Header.Get("Authorization") != "Bearer owned-access-token-rotated" {
			http.Error(w, "bad bearer", 401)
			return
		}
		switch r.URL.Path {
		case "/api/v1/user":
			json.NewEncoder(w).Encode(map[string]any{"id": 77, "login": "owner"})
		case "/api/v1/user/orgs":
			json.NewEncoder(w).Encode([]any{})
		case "/api/v1/repos/owner/new-repo":
			if creates.Load() == 0 {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(remoteRepository{ID: 99, FullName: "owner/new-repo", Private: true})
		case "/api/v1/user/repos":
			var input struct {
				Name    string
				Private bool
			}
			json.NewDecoder(r.Body).Decode(&input)
			if input.Name != "new-repo" || !input.Private {
				http.Error(w, "wrong repository", 400)
				return
			}
			creates.Add(1)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(remoteRepository{ID: 99, FullName: "owner/new-repo", Private: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer forge.Close()
	config := filepath.Join(t.TempDir(), "models.json")
	os.WriteFile(config, []byte(`[{"id":"owned-model","name":"Owned model","endpoint":"https://owned-model.test","key_file":"/never-read","models":[{"requested":"owned","allowed_actual":["owned"]}],"workspaces":["setup"]}]`), 0600)
	gateway.ConfigFile = config
	app := map[string]any{"name": "Owned Gitea", "instance": forge.URL, "client_id": "owned-client", "client_secret": "owned-client-secret", "expected_version": "0"}
	request("setup-manager", "POST", "/forge/oauth/apps/owned", app, 403)
	request("setup-admin", "POST", "/forge/oauth/apps/owned", app, 200)
	request("setup-admin", "POST", "/forge/oauth/apps/owned", app, 409)
	request("setup-editor", "GET", "/forge/workspaces/setup/oauth/apps", nil, 403)
	request("setup-manager", "GET", "/forge/oauth/apps", nil, 403)
	request("setup-manager", "GET", "/forge/workspaces/setup/oauth/apps", nil, 200)
	base := "/forge/workspaces/setup/oauth"
	start := func(actor string) map[string]string {
		out := map[string]string{}
		json.Unmarshal(request(actor, "POST", base+"/owned/start", map[string]any{}, 200), &out)
		return out
	}
	complete := func(out map[string]string) {
		t.Helper()
		res, err := http.Get(out["authorization_url"])
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("callback: %d", res.StatusCode)
		}
	}
	request("setup-editor", "POST", base+"/owned/start", map[string]any{}, 403)
	request("setup-outsider", "GET", "/forge/workspaces/setup/oauth/apps", nil, 403)
	first := start("setup-manager")
	complete(first)
	request("setup-admin", "GET", base+"/sessions/"+first["id"], nil, 403)
	status := request("setup-manager", "GET", base+"/sessions/"+first["id"], nil, 200)
	if !bytes.Contains(status, []byte(`"login":"owner"`)) {
		t.Fatal("missing account review")
	}
	accept := mutation("0")
	accept["name"] = "Shared account"
	accept["allow_create"] = true
	accepted := request("setup-manager", "POST", base+"/sessions/"+first["id"]+"/accept", accept, 200)
	var saved struct{ Data struct{ Version string } }
	json.Unmarshal(accepted, &saved)
	id := "oauth-" + first["id"]
	connection, _, err := s.Connection(ctx, "setup", id)
	if err != nil || connection.Auth != "oauth" || connection.Account != "owner" {
		t.Fatalf("not stored: %+v %v", connection, err)
	}
	// PKCE state is single use, and an already accepted session cannot create another connection.
	authURL, _ := url.Parse(first["authorization_url"])
	request("setup-manager", "GET", "/forge/oauth/callback?state="+url.QueryEscape(authURL.Query().Get("state"))+"&code=replay", nil, 400)
	request("setup-manager", "POST", base+"/sessions/"+first["id"]+"/accept", accept, 409)
	var encrypted []byte
	store.Database().QueryRow(ctx, `SELECT credential FROM forge_connections WHERE workspace_id='setup' AND id=$1`, id).Scan(&encrypted)
	if bytes.Contains(encrypted, []byte("owned-access-token")) {
		t.Fatal("unencrypted credential")
	}
	credentials, _ := s.open("setup", id, encrypted)
	credentials.OAuth.Expires = time.Now().Add(-time.Minute)
	encrypted, _ = s.seal("setup", id, credentials)
	store.Database().Exec(ctx, `UPDATE forge_connections SET credential=$2 WHERE workspace_id='setup' AND id=$1`, id, encrypted)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, client, err := s.Connection(ctx, "setup", id)
			if err != nil {
				t.Error(err)
				return
			}
			var account oauthAccount
			if err = client.Get(ctx, "/user", &account); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh dispatched %d times", refreshes.Load())
	}
	store.Database().QueryRow(ctx, `SELECT credential FROM forge_connections WHERE workspace_id='setup' AND id=$1`, id).Scan(&encrypted)
	rotated, _ := s.open("setup", id, encrypted)
	if rotated.Token != "owned-access-token-rotated" || rotated.OAuth.Refresh != "owned-refresh-token-rotated" {
		t.Fatal("rotated credentials not preserved")
	}
	// Updating only sharing metadata neither refreshes nor overwrites rotated credentials.
	update := mutation(connection.Version)
	update["name"] = "Renamed"
	update["instance"] = forge.URL
	update["allow_create"] = false
	request("setup-manager", "POST", "/forge/workspaces/setup/connections/"+id, update, 200)
	connection, _, _ = s.Connection(ctx, "setup", id)
	var reauth map[string]string
	json.Unmarshal(request("setup-manager", "POST", base+"/owned/start", map[string]any{"connection": id, "expected_version": connection.Version}, 200), &reauth)
	complete(reauth)
	renew := mutation(connection.Version)
	renew["name"] = "Renewed"
	renew["allow_create"] = true
	request("setup-manager", "POST", base+"/sessions/"+reauth["id"]+"/accept", renew, 200)
	if c, _, err := s.Connection(ctx, "setup", id); err != nil || c.Name != "Renewed" {
		t.Fatal("reconnect replaced stable connection identity")
	}
	blocked := start("setup-manager")
	complete(blocked)
	store.Database().Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='setup' AND user_id='setup-manager'`)
	request("setup-manager", "POST", base+"/sessions/"+blocked["id"]+"/accept", accept, 403)
	store.Database().Exec(ctx, `UPDATE access_members SET role='admin' WHERE workspace_id='setup' AND user_id='setup-manager'`)
	store.Database().Exec(ctx, `UPDATE forge_oauth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, blocked["id"])
	request("setup-manager", "POST", base+"/sessions/"+blocked["id"]+"/accept", accept, 409)
	for _, reason := range []string{"app-change", "expiry", "revoked", "cancel"} {
		pending := start("setup-manager")
		switch reason {
		case "app-change":
			store.Database().Exec(ctx, `UPDATE forge_oauth_apps SET version=version+1 WHERE id='owned'`)
		case "expiry":
			store.Database().Exec(ctx, `UPDATE forge_oauth_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, pending["id"])
		case "revoked":
			store.Database().Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='setup' AND user_id='setup-manager'`)
		case "cancel":
			u, _ := url.Parse(pending["authorization_url"])
			request("setup-manager", "GET", "/forge/oauth/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&error=access_denied", nil, 400)
		}
		if reason != "cancel" {
			res, err := http.Get(pending["authorization_url"])
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != 400 {
				t.Fatalf("accepted %s callback", reason)
			}
		}
		if reason == "revoked" {
			store.Database().Exec(ctx, `UPDATE access_members SET role='admin' WHERE workspace_id='setup' AND user_id='setup-manager'`)
		}
	}
	request("setup-manager", "GET", "/server-models/workspaces/setup", nil, 403)
	request("setup-admin", "GET", "/server-models/workspaces/setup", nil, 200)
	request("setup-admin", "POST", "/server-models/workspaces/setup/owned-model", map[string]any{"allowed": false, "expected_version": "0"}, 200)
	if _, err = gateway.Connection("setup", "owned-model", ""); err == nil {
		t.Fatal("revocation ignored")
	}
	list, _ := gateway.Connections("setup")
	if len(list) != 0 {
		t.Fatal("file grant bypassed explicit revocation")
	}
	request("setup-admin", "POST", "/server-models/workspaces/setup/owned-model", map[string]any{"allowed": true, "expected_version": "0"}, 409)
	request("setup-admin", "POST", "/server-models/workspaces/setup-ui/owned-model", map[string]any{"allowed": true, "expected_version": "0"}, 200)
	list, _ = gateway.Connections("setup-ui")
	if len(list) != 1 {
		t.Fatal("UI grant not applied")
	}
	request("setup-admin", "POST", "/server-models/workspaces/setup-ui/owned-model", map[string]any{"allowed": false, "expected_version": "1"}, 200)
	// Execute actual React forms over this owned HTTP API. No user app/session or real model is involved.
	if os.Getenv("KOUJOU_SETUP_UI_TEST") == "1" {
		clientDir, _ := filepath.Abs("../../../client")
		command := exec.Command("node", filepath.Join(clientDir, "node_modules/vitest/vitest.mjs"), "run", "src/workspace-setup.test.tsx", "--maxWorkers", "1")
		command.Dir = clientDir
		command.Env = append(os.Environ(), "KOUJOU_SETUP_API="+api.URL, "KOUJOU_SETUP_FORGE="+forge.URL)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("owned UI flow: %v\n%s", err, output)
		}
		t.Log(string(output))
	}
	var history string
	store.Database().QueryRow(ctx, `SELECT COALESCE(string_agg(payload::text,''),'') FROM shared_changes WHERE workspace_id IN ('setup','setup-ui')`).Scan(&history)
	if strings.Contains(history, "owned-access-token") || strings.Contains(history, "owned-refresh-token") || strings.Contains(history, "owned-client-secret") {
		t.Fatal("OAuth secret reached history")
	}
	t.Logf("OAuth PKCE, account review, current rights, encrypted storage, serialized refresh, bounded catalogs and assignments passed (%d exchanges)", exchanges.Load())
}
