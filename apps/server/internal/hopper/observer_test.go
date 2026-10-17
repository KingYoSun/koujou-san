package hopper

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func checkObserverPipeline(t *testing.T, s *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var latest atomic.Int64
	latest.Store(101)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/7/access_tokens" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "fixture-read-only", "expires_at": time.Now().Add(time.Hour), "permissions": map[string]string{"metadata": "read", "issues": "read", "pull_requests": "read", "contents": "read", "actions": "read"}, "repositories": []any{map[string]any{"id": 8, "full_name": "fixture/repo"}}})
			return
		}
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-read-only" {
			t.Error("unexpected external write or grant", r.Method)
			w.WriteHeader(403)
			return
		}
		if strings.Contains(r.URL.Path, "/actions/workflows/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": latest.Load(), "workflow_id": 42}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "jobs": []any{map[string]any{"id": 1, "name": "nightly", "status": "completed", "conclusion": "failure", "run_attempt": 1}}})
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/fixture/repo/actions/runs/"))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "workflow_id": 42, "run_attempt": 1, "head_sha": strings.Repeat("a", 40), "status": "completed", "conclusion": "failure", "html_url": fmt.Sprintf("https://github.com/fixture/repo/actions/runs/%d", id)})
	}))
	defer source.Close()
	backend, _ := url.Parse(source.URL)
	original := http.DefaultTransport
	http.DefaultTransport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			copy := r.Clone(r.Context())
			u := *r.URL
			u.Scheme = backend.Scheme
			u.Host = backend.Host
			copy.URL = &u
			copy.Host = backend.Host
			return original.RoundTrip(copy)
		}
		return original.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = original }()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "synthetic.pem")
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "observer.json")
	config := githubobserve.Config{AppID: "fixture-app", PrivateKeyFile: keyPath, IntervalSeconds: 1, Targets: []githubobserve.Target{{ID: "pipeline-latest", Kind: "actions", WorkflowID: 42, Event: "schedule", Repository: "fixture/repo", RepositoryID: 8, InstallationID: 7}}, Bindings: []githubobserve.Binding{{WorkspaceID: "one", TargetID: "pipeline-latest"}}}
	if err = os.WriteFile(configPath, raw(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOUJOU_GITHUB_CONFIG", configPath)
	loaded, err := githubobserve.Environment()
	if err != nil {
		t.Fatal(err)
	}
	observer, err := githubobserve.New(ctx, s.Store, s.Access, loaded)
	if err != nil {
		t.Fatal(err)
	}
	observer.Accepted = s.GitHubAccepted
	p := access.Principal{UserID: "admin"}
	template, err := s.Workflows.Read(ctx, p, "one", Ref{ID: "run-definition:repair", Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	d := Template()
	d.Name = "read-only observer pipeline"
	d.Problem = nil
	d.Targets = []Target{{"github", "pipeline-latest"}}
	d.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
	d.Action.Template = template.Ref
	definition, err := accepted(s.Put(ctx, p, "one", "hopper:pipeline", Write{Mutation: mutation("pipeline-definition"), Value: raw(d)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = accepted(s.Put(ctx, p, "one", "hopper-active:pipeline", Write{Mutation: mutation("pipeline-enable"), Value: raw(Activation{Definition: definition, Enabled: true})})); err != nil {
		t.Fatal(err)
	}
	observer.Start(ctx)
	s.Start(ctx)
	wait := func(run int64) Evaluation {
		t.Helper()
		until := time.Now().Add(15 * time.Second)
		for time.Now().Before(until) {
			var b []byte
			err := s.Store.Database().QueryRow(ctx, `SELECT r.payload FROM hopper_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id='one' AND r.kind='evaluation' AND r.payload->'definition'->>'id'=$1 AND r.payload->'source'->'facts'->'report'->>'run_id'=$2 AND r.payload->>'state'='published'`, definition.ID, strconv.FormatInt(run, 10)).Scan(&b)
			if err == nil {
				var e Evaluation
				_ = json.Unmarshal(b, &e)
				return e
			}
			time.Sleep(50 * time.Millisecond)
		}
		var reason string
		_ = s.Store.Database().QueryRow(ctx, `SELECT reason FROM github_targets WHERE id='pipeline-latest'`).Scan(&reason)
		t.Fatal("observer to Hopper pipeline did not publish", reason)
		return Evaluation{}
	}
	first := wait(101)
	if _, err = accepted(s.Decide(ctx, p, "one", "hopper-chain:"+first.Root, Decision{Mutation: mutation("pipeline-stop-first"), Action: "stop"})); err != nil {
		t.Fatal(err)
	}
	latest.Store(102)
	second := wait(102)
	if first.Root == second.Root || first.Destination == nil || second.Destination == nil || first.Destination.ID == second.Destination.ID {
		t.Fatal("next period reset or reused the previous chain", first.Root, second.Root)
	}
	cancel()
	t.Log("owned HTTP GitHub read/token exchange -> periodic reconciliation -> result transaction -> persistent Worker -> two Runs; stopped first root does not stop next cycle; no client or LLM gateway")
}
