package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Optional Computer Use fixture. Only this isolated test identity is forwarded;
// no development-user credential or browser certificate exception is needed.
func inspectUI(t *testing.T, client *http.Client, target string, tokens, bob auth.Tokens, webTokens auth.Tokens, certificate []byte, cfg auth.Config, service *Service, githubMux *http.ServeMux) {
	t.Helper()
	if os.Getenv("KOUJOU_TEST_MCP_UI") != "1" {
		return
	}
	if _, err := os.Stat("../../../client/dist/index.html"); err != nil {
		t.Fatal("build the common UI before UI inspection")
	}

	ctx, cancel := context.WithCancel(context.Background())
	var githubConfig *githubobserve.Config
	if os.Getenv("KOUJOU_TEST_GITHUB_LIVE") == "1" {
		var err error
		githubConfig, err = githubobserve.Environment()
		if err != nil || githubConfig == nil {
			t.Fatal("GitHub UI test requires an explicit App configuration")
		}
		// Only fixture Workspace bindings are replaced. App/repository permissions
		// remain exactly those of the explicit read-only live configuration.
		githubConfig.Bindings = nil
		for _, target := range githubConfig.Targets {
			githubConfig.Bindings = append(githubConfig.Bindings, githubobserve.Binding{WorkspaceID: "mcp-one", TargetID: target.ID, ExpectedCommit: "ec3a86cf6a02bfd97079fd7a2e918afd06096c3c", RequiredJobs: []string{"web-e2e / android-web-e2e"}})
		}
	}
	observer, err := githubobserve.New(ctx, service.Access.Store, service.Access, githubConfig)
	if err != nil {
		t.Fatal(err)
	}
	observer.Register(githubMux)
	observer.Start(ctx)
	user, err := service.Auth.User(ctx, tokens.Access)
	if err != nil {
		t.Fatal(err)
	}
	principal := access.Principal{UserID: user.ID}
	write := func(kind, id string, value any) contexts.Ref {
		raw, _ := json.Marshal(value)
		result, err := service.Contexts.Put(ctx, principal, "mcp-one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: apicontract.Mutation{OperationID: "ui-fixture-" + id, ExpectedVersion: "0"}, Value: raw})
		if err != nil || result.Error != nil || result.Data == nil {
			t.Fatal("UI fixture preparation failed", id, err)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: result.Data.Version}
	}
	text := "受入確認: 二つの段落から参照する共有の文。"
	shared := write("sentence", "ui-shared", contexts.Unit{Kind: "sentence", Name: "受入: 共有する文", Text: &text})
	digest := strings.Repeat("a", 64)
	originText := "受入確認: 由来と固定添付を持つ別の文。"
	origin := write("sentence", "ui-origin", contexts.Unit{Kind: "sentence", Name: "受入: 由来と添付", Text: &originText, Origin: &contexts.Origin{Kind: "output", ID: "original-run", Revision: "output-v1", Digest: digest, Path: "/result"}, Attachments: []contexts.Fixed{{URI: "fixture://code", Revision: "code-v1", Range: "lines:1-2", Digest: digest, Delivery: "reference", State: "ready"}}})
	write("sentence", "ui-unfetched", contexts.Unit{Kind: "sentence", Name: "受入: 未取得の添付", Text: &text, Attachments: []contexts.Fixed{{URI: "fixture://code", Revision: "code-v1", Range: "lines:1-2", Digest: digest, Delivery: "reference", State: "unfetched"}}})
	write("sentence", "ui-private", contexts.Unit{Kind: "sentence", Name: "受入: 権限変更の文", Text: &text})
	otherUser, err := service.Auth.User(ctx, bob.Access)
	if err != nil {
		t.Fatal(err)
	}
	a := write("paragraph", "ui-p1", contexts.Unit{Kind: "paragraph", Name: "受入: 一つ目の段落", Children: []contexts.Ref{shared, origin}})
	b := write("paragraph", "ui-p2", contexts.Unit{Kind: "paragraph", Name: "受入: 二つ目の段落", Children: []contexts.Ref{shared}})
	section := write("section", "ui-section", contexts.Unit{Kind: "section", Title: "受入: 判断の基準", Children: []contexts.Ref{a, b}})
	document := write("document", "ui-document", contexts.Unit{Kind: "document", Title: "受入: 製品方針", Children: []contexts.Ref{section}})
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("ui-scope-%03d", i)
		kind := "issue"
		if i == 0 {
			kind = "product"
		}
		scope := write("scope", id, contexts.Scope{Kind: kind, Name: fmt.Sprintf("受入Scope %03d", i)})
		if i < 2 {
			write("membership", fmt.Sprintf("ui-membership-%d", i), contexts.Membership{Scope: contexts.Identity{Kind: kind, ID: scope.ID}, Context: document.Identity(), Present: true})
		}
	}
	if err := service.Search.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Graph.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	var projectionPaused atomic.Bool
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if projectionPaused.Load() {
					continue
				}
				_ = service.Search.Sync(ctx)
				_ = service.Graph.Sync(ctx)
			}
		}
	}()
	defer func() { cancel(); workers.Wait() }()

	done := make(chan struct{}, 1)
	var traceLock sync.Mutex
	trace := []map[string]any{}
	defer func() {
		raw, _ := json.MarshalIndent(trace, "", "  ")
		_ = os.WriteFile("../../../../.cache/issue12-ui-requests.json", raw, 0600)
	}()
	gateway := func(current auth.Tokens) *httptest.Server {
		var tokenLock sync.Mutex
		expires := time.Now().Add(-time.Second) // Exercise a real refresh before the first UI request.
		bearer := func() (string, error) {
			tokenLock.Lock()
			defer tokenLock.Unlock()
			if time.Now().Add(30 * time.Second).After(expires) {
				response, err := client.PostForm(cfg.Issuer+"oauth2/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {current.Refresh}, "client_id": {cfg.NativeClient}})
				if err != nil {
					return "", err
				}
				defer response.Body.Close()
				if response.StatusCode != 200 {
					return "", fmt.Errorf("fixture token refresh rejected")
				}
				if err = json.NewDecoder(response.Body).Decode(&current); err != nil {
					return "", err
				}
				expires = time.Now().Add(time.Duration(current.ExpiresIn) * time.Second)
			}
			return current.Access, nil
		}
		var offline atomic.Bool
		base, _ := url.Parse(target)
		proxy := httputil.NewSingleHostReverseProxy(base)
		proxy.Transport = client.Transport
		direct := proxy.Director
		proxy.Director = func(r *http.Request) {
			direct(r)
			r.Host = base.Host
			r.Header.Del("Cookie")

			r.Header.Set("Origin", target)
		}
		files := http.FileServer(http.Dir("../../../client/dist"))
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/__fixture/finish" && r.Method == "POST" {
				select {
				case done <- struct{}{}:
				default:
				}
				w.WriteHeader(204)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/__fixture/") && r.Method == "POST" {
				switch strings.TrimPrefix(r.URL.Path, "/__fixture/") {

				case "deny-bob", "allow-bob":
					allow := strings.HasSuffix(r.URL.Path, "allow-bob")
					grants := []access.ResourceGrant{{UserID: user.ID, Read: true, Update: true}}
					if allow {
						grants = append(grants, access.ResourceGrant{UserID: otherUser.ID, Read: true, Update: true})
					}
					err := pgx.BeginFunc(ctx, service.Access.Store.Database(), func(tx pgx.Tx) error {
						return service.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: "context:ui-private", Restricted: true}, grants)
					})
					if err != nil {
						http.Error(w, "fixture policy failed", 500)
						return
					}
				case "offline-on":
					offline.Store(true)
				case "offline-off":
					offline.Store(false)
				case "projection-pause":
					projectionPaused.Store(true)
				case "projection-resume":
					projectionPaused.Store(false)
				case "search-stop":
					projectionPaused.Store(true)
					if err := service.Search.Close(); err != nil {
						http.Error(w, "fixture failed", 500)
						return
					}
				case "search-start":
					if err := service.Search.Rebuild(ctx); err != nil {
						http.Error(w, "fixture failed", 500)
						return
					}
					projectionPaused.Store(false)
				default:
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(204)
				return
			}
			if offline.Load() {
				http.Error(w, "owned fixture connection unavailable", 503)
				return
			}
			entry := map[string]any{"method": r.Method, "path": r.URL.Path, "request_bytes": r.ContentLength}
			if strings.HasSuffix(r.URL.Path, "/pull") {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "fixture failed", 500)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				var body struct {
					Mode  string              `json:"mode"`
					Known []contexts.Identity `json:"known"`
				}
				_ = json.Unmarshal(raw, &body)
				entry["mode"] = body.Mode
				entry["known"] = len(body.Known)
			}
			traceLock.Lock()
			trace = append(trace, entry)
			traceLock.Unlock()
			for _, prefix := range []string{"/auth/me", "/auth/config", "/auth/csrf", "/access/", "/mcp/control/", "/contexts/", "/sync/", "/search/", "/graph/", "/github/", "/usage/", "/automation/", "/runs/", "/healthz", "/readyz"} {
				if strings.HasPrefix(r.URL.Path, prefix) {
					token, err := bearer()
					if err != nil {
						http.Error(w, "fixture authentication unavailable", 503)
						return
					}
					r.Header.Set("Authorization", "Bearer "+token)
					proxy.ServeHTTP(w, r)
					return
				}
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				files.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, "../../../client/dist/index.html")
		}))
	}
	fixture := gateway(webTokens)
	defer fixture.Close()
	second := gateway(bob)
	defer second.Close()
	root := filepath.Clean("../../../../.cache")
	caFile := filepath.Join(root, "issue12-ui-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0600); err != nil {
		t.Fatal(err)
	}
	private := map[string]any{"api": target + "/", "issuer": cfg.Issuer, "native_refresh": tokens.Refresh, "alice_url": fixture.URL, "bob_url": second.URL}
	privateRaw, _ := json.Marshal(private)
	if err := os.WriteFile(filepath.Join(root, "issue12-ui-private.json"), privateRaw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("Second User browser fixture: %s; native metadata saved privately to .cache", second.URL)

	t.Logf("Computer Use fixture: %s (isolated identity, expires after thirty minutes); POST /__fixture/finish ends inspection", fixture.URL)
	select {
	case <-done:
		t.Log("Computer Use inspection finished")
	case <-time.After(30 * time.Minute):
		t.Fatal("UI inspection timed out")
	}
}
