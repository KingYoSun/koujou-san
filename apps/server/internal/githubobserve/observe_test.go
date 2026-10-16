package githubobserve

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

const fixtureSHA = "ec3a86cf6a02bfd97079fd7a2e918afd06096c3c"

type githubFixture struct {
	mu       sync.Mutex
	key      *rsa.PrivateKey
	calls    []string
	attempt  int
	mode     string
	complete bool
	jobs     int
}

func fixture(t *testing.T) (*githubFixture, *httptest.Server, *Config) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &githubFixture{key: key, attempt: 1, jobs: 2}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	c := &Config{AppID: "fixture-app", IntervalSeconds: 60, key: key, secret: []byte("fixture-webhook-only"), Targets: []Target{
		{ID: "action", InstallationID: 7, RepositoryID: 8, Repository: "fixture/repo", Kind: "actions", ExternalID: "123"},
		{ID: "commit", InstallationID: 7, RepositoryID: 8, Repository: "fixture/repo", Kind: "commit", ExternalID: fixtureSHA},
		{ID: "issue", InstallationID: 7, RepositoryID: 8, Repository: "fixture/repo", Kind: "issue", ExternalID: "12"},
		{ID: "pr", InstallationID: 7, RepositoryID: 8, Repository: "fixture/repo", Kind: "pr", ExternalID: "13"},
	}, Bindings: []Binding{{WorkspaceID: "one", TargetID: "action", ExpectedCommit: strings.Repeat("a", 40), RequiredJobs: []string{"required"}}, {WorkspaceID: "two", TargetID: "action"}, {WorkspaceID: "one", TargetID: "commit"}, {WorkspaceID: "one", TargetID: "issue"}, {WorkspaceID: "one", TargetID: "pr"}}}
	if err = c.validate(); err != nil {
		t.Fatal(err)
	}
	return f, server, c
}
func (f *githubFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
	if strings.HasPrefix(r.URL.Path, "/app/installations/") {
		jwt := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(jwt) != 3 {
			http.Error(w, "jwt", 401)
			return
		}
		signature, _ := base64.RawURLEncoding.DecodeString(jwt[2])
		digest := sha256.Sum256([]byte(jwt[0] + "." + jwt[1]))
		if rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			http.Error(w, "signature", 401)
			return
		}
		var payload struct {
			Repositories []int64           `json:"repository_ids"`
			Permissions  map[string]string `json:"permissions"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.Repositories) != 1 || payload.Repositories[0] != 8 {
			http.Error(w, "repository", 403)
			return
		}
		for _, v := range payload.Permissions {
			if v != "read" {
				http.Error(w, "permission", 403)
				return
			}
		}
		if len(payload.Permissions) != 5 {
			http.Error(w, "permissions", 403)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "fixture-opaque-token", "expires_at": time.Now().Add(time.Hour), "permissions": payload.Permissions, "repositories": []any{map[string]any{"id": 8, "full_name": "fixture/repo"}}})
		return
	}
	if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-opaque-token" {
		http.Error(w, "forbidden", 403)
		return
	}
	if f.mode == "auth" {
		http.Error(w, "denied", 401)
		return
	}
	if f.mode == "rate" {
		w.Header().Set("Retry-After", "61")
		w.WriteHeader(429)
		return
	}
	if f.mode == "unavailable" {
		w.WriteHeader(503)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/jobs") {
		attempt := f.attempt
		if strings.Contains(r.URL.Path, "/attempts/1/") {
			attempt = 1
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if f.mode == "interrupt" && page == 2 {
			w.WriteHeader(503)
			return
		}
		jobs := []Job{}
		for i := (page - 1) * 100; i < f.jobs && i < page*100; i++ {
			name := fmt.Sprintf("job-%d", i)
			conclusion := "success"
			if i == 0 {
				name = "required"
				conclusion = "skipped"
			}
			jobs = append(jobs, Job{int64(i + 1), name, "completed", conclusion, attempt})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": f.jobs, "jobs": jobs})
		return
	}
	state := "in_progress"
	conclusion := ""
	if f.complete {
		state = "completed"
		conclusion = "success"
	}
	value := source{ID: 123, Title: "fixture", State: "open", URL: "https://github.com/fixture/repo", Updated: "2026-10-15T19:20:12Z"}
	switch {
	case strings.Contains(r.URL.Path, "/actions/runs/"):
		value.State = ""
		value.Status = state
		value.Conclusion = conclusion
		value.HeadSHA = fixtureSHA
		value.WorkflowID = 42
		value.Attempt = f.attempt
		if strings.HasSuffix(r.URL.Path, "/attempts/1") {
			value.Attempt = 1
			value.Status = "completed"
			value.Conclusion = "failure"
		}
	case strings.Contains(r.URL.Path, "/commits/"):
		value.SHA = fixtureSHA
		value.Commit.Message = "fixture commit\nbody"
		value.Commit.Committer.Date = value.Updated
	case strings.Contains(r.URL.Path, "/pulls/"):
		value.Head.SHA = fixtureSHA
		value.MergeCommit = strings.Repeat("b", 40)
		if f.complete {
			value.State = "closed"
		}
		value.Merged = f.complete
	}
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	etag := hex.EncodeToString(hash[:])
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	_, _ = w.Write(raw)
}

func TestGithubRemote(t *testing.T) {
	f, server, c := fixture(t)
	r := newRemote(c)
	r.base = server.URL
	for _, target := range c.Targets {
		v, err := r.observe(context.Background(), target)
		if err != nil || v.URL == "" || v.SourceVersion == "" {
			t.Fatalf("%s: %v", target.Kind, err)
		}
	}
	f.mu.Lock()
	f.attempt = 2
	f.complete = true
	f.mu.Unlock()
	merged, mergeErr := r.observe(context.Background(), c.Targets[3])
	if mergeErr != nil || merged.State != "merged" {
		t.Fatal("merged PR was displayed as an unmerged closed PR")
	}
	v, err := r.observe(context.Background(), c.Targets[0])
	if err != nil || v.Attempt != 2 || v.Conclusion != "success" || v.Jobs[0].Conclusion != "skipped" || v.Commit != fixtureSHA {
		t.Fatalf("attempt result: %+v %v", v, err)
	}
	pinned := c.Targets[0]
	pinned.Attempt = 1
	v, err = r.observe(context.Background(), pinned)
	if err != nil || v.Attempt != 1 || v.Conclusion != "failure" {
		t.Fatalf("pinned attempt: %+v %v", v, err)
	}
	wrongName := c.Targets[0]
	wrongName.Repository = "outside/public"
	if _, err = r.observe(context.Background(), wrongName); err == nil {
		t.Fatal("repository ID/name mismatch accepted")
	}
	f.mu.Lock()
	f.jobs = 101
	f.mode = "interrupt"
	f.mu.Unlock()
	if _, err = r.observe(context.Background(), c.Targets[0]); err == nil || err.Error() != "source_unavailable" {
		t.Fatal("partial jobs accepted")
	}
	f.mu.Lock()
	f.mode = "rate"
	f.mu.Unlock()
	if _, err = r.observe(context.Background(), c.Targets[0]); err == nil || time.Until(r.resumeAt) < time.Minute {
		t.Fatal("rate limit ignored")
	}
	forbidden := c.Targets[0]
	forbidden.RepositoryID = 999
	separate := newRemote(c)
	separate.base = server.URL
	if _, err = separate.observe(context.Background(), forbidden); err == nil {
		t.Fatal("out-of-installation repository accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "GET /repos/") && call != "POST /app/installations/7/access_tokens" {
			t.Fatal("unexpected GitHub write")
		}
	}
	t.Log("read-only JWT exchange, four kinds, exact attempts, skipped job, interrupted page and API backoff passed")
}

func TestGithubPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:github")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.Database()
	for _, u := range []string{"alice", "bob", "outsider"} {
		if _, err = db.Exec(ctx, "INSERT INTO auth_users VALUES($1,'github-fixture',$1,$1||'@koujou.test',$1,false)", u); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []string{"one", "two"} {
		if _, err = db.Exec(ctx, "INSERT INTO access_workspaces VALUES($1,$1,'alice')", w); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES($1,'alice','admin')", w); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','bob','viewer')")
	if err != nil {
		t.Fatal(err)
	}
	a := access.New(store, []byte(strings.Repeat("g", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) { return r.Header.Get("Fixture-User"), true })
	f, server, c := fixture(t)
	observer := func() *Service {
		t.Helper()
		s, e := New(ctx, store, a, c)
		if e != nil {
			t.Fatal(e)
		}
		s.remote.base = server.URL
		return s
	}
	s := observer()
	count := func(query string) int {
		t.Helper()
		var n int
		if e := db.QueryRow(ctx, query).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 5; i++ {
			worked, e := s.step(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if !worked {
				return
			}
		}
		t.Fatal("worker did not finish bounded fixture")
	}
	drain()
	if count("SELECT count(*) FROM github_observations") != 4 {
		t.Fatal("coalesced bindings caused duplicate observation")
	}
	mux := http.NewServeMux()
	s.Register(mux)
	request := func(user, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Fixture-User", user)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	view := func() View {
		t.Helper()
		w := request("bob", "/github/workspaces/one/observations")
		var response apicontract.Response[[]View]
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data == nil {
			t.Fatalf("list status %d", w.Code)
		}
		for _, v := range *response.Data {
			if v.Target.ID == "action" {
				return v
			}
		}
		t.Fatal("action missing")
		return View{}
	}
	first := view()
	if first.Snapshot.Attempt != 1 || first.ExpectedCommit == fixtureSHA || first.Snapshot.Jobs[0].Conclusion != "skipped" {
		t.Fatal("target evidence lost")
	}
	// A notification only marks the repository due, regardless of its delivery ID,
	// stale state, or order. The worker always reads the fixture's current state.
	notify := func(delivery string) {
		t.Helper()
		body := `{"installation":{"id":7},"repository":{"id":8},"workflow_run":{"conclusion":"failure","run_attempt":1}}`
		mac := hmac.New(sha256.New, c.secret)
		_, _ = mac.Write([]byte(body))
		r := httptest.NewRequest("POST", "/github/webhook", strings.NewReader(body))
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatal(w.Code)
		}
	}
	f.mu.Lock()
	f.attempt = 2
	f.complete = true
	f.mu.Unlock()
	notify("new")
	notify("new")
	notify("old")
	drain()
	current := view()
	if current.Snapshot.Attempt != 2 || current.Snapshot.Conclusion != "success" || current.Version != 2 {
		t.Fatal("notification overwrote current GitHub state")
	}
	notify("old")
	drain()
	if count("SELECT count(*) FROM github_observations WHERE target_id='action'") != 2 {
		t.Fatal("unchanged polling appended history")
	}
	current = view() // Unchanged content still has a new successful fetch time.
	for _, mode := range []string{"unavailable", "auth", "interrupt"} {
		f.mu.Lock()
		f.mode = mode
		f.jobs = 101
		f.mu.Unlock()
		notify(mode)
		drain()
		failed := view()
		if failed.State != "unknown" || failed.Snapshot.Attempt != 2 || !failed.ObservedAt.Equal(*current.ObservedAt) {
			t.Fatalf("%s lost prior observation", mode)
		}
	}
	if request("outsider", "/github/workspaces/one/observations").Code != 403 || request("bob", "/github/workspaces/two/observations/action/history").Code != 403 {
		t.Fatal("Workspace boundary failed")
	}
	_, err = db.Exec(ctx, "UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id='github:action'")
	if err != nil {
		t.Fatal(err)
	}
	if request("bob", "/github/workspaces/one/observations/action/history").Code != 403 {
		t.Fatal("restricted history exposed")
	}
	_, err = db.Exec(ctx, "UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id='github:action'")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.mode = ""
	f.jobs = 2
	f.complete = false
	f.attempt = 3
	f.mu.Unlock()
	c.IntervalSeconds = 1 // Short fixture clock; product default remains 60 seconds.
	s = observer()
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.Start(workerCtx)
	// No browser or native client runs while this background worker observes.
	deadline := time.Now().Add(8 * time.Second)
	for count("SELECT count(*) FROM github_observations WHERE target_id='action' AND snapshot->>'run_attempt'='3'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("server-only observation timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	f.complete = true
	f.mu.Unlock()
	for count("SELECT count(*) FROM github_observations WHERE target_id='action' AND snapshot->>'run_attempt'='3' AND snapshot->>'state'='completed'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("server-only completion timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	s.mu.Lock()
	s.mu.Unlock()
	s = observer()
	mux = http.NewServeMux()
	s.Register(mux)
	if view().Snapshot.Attempt != 3 || view().Snapshot.State != "completed" {
		t.Fatal("restart lost observation")
	}
	history := request("bob", "/github/workspaces/one/observations/action/history")
	if history.Code != 200 || !strings.Contains(history.Body.String(), `"run_attempt":1`) || !strings.Contains(history.Body.String(), `"run_attempt":3`) {
		t.Fatal("history missing attempts")
	}
	bad := httptest.NewRecorder()
	mux.ServeHTTP(bad, httptest.NewRequest("POST", "/github/webhook", strings.NewReader("{}")))
	if bad.Code != 401 {
		t.Fatal("unsigned webhook accepted")
	}
	t.Log("shared bindings, duplicate/delayed/reversed notifications, last observation retention, read ACL, server-only reconciliation and restart passed")
}

func TestGithubLive(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_GITHUB_LIVE") != "1" {
		t.Skip("explicit GitHub App live test only")
	}
	c, err := Environment()
	if err != nil || c == nil || len(c.Targets) == 0 {
		t.Fatal("configure KOUJOU_GITHUB_CONFIG without printing credentials")
	}
	r := newRemote(c)
	type requestTrace struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Status int    `json:"status"`
	}
	trace := []requestTrace{}
	r.client.Transport = traceTransport(func(req *http.Request) (*http.Response, error) {
		response, e := http.DefaultTransport.RoundTrip(req)
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		trace = append(trace, requestTrace{req.Method, req.URL.RequestURI(), status})
		return response, e
	})
	for _, target := range c.Targets {
		snapshot, e := r.observe(context.Background(), target)
		if e != nil {
			t.Fatalf("target %s: %v", target.ID, e)
		}
		t.Logf("repository_id=%d kind=%s external_id=%s commit=%s workflow_id=%d run_id=%d attempt=%d conclusion=%s source_version=%s observed_at=%s", target.RepositoryID, target.Kind, target.ExternalID, snapshot.Commit, snapshot.WorkflowID, snapshot.RunID, snapshot.Attempt, snapshot.Conclusion, snapshot.SourceVersion, time.Now().UTC().Format(time.RFC3339))
	}
	// The public name is deliberately mismatched against the allowed repository
	// ID. Even GitHub's public-read capability must not broaden this connection.
	outside := c.Targets[0]
	outside.Repository = "KingYoSun/koujou-san"
	if _, e := r.observe(context.Background(), outside); e == nil || e.Error() != "repository_not_allowed" {
		t.Fatal("outside repository name was not rejected before GET")
	}
	for _, entry := range trace {
		if entry.Method != "GET" && !(entry.Method == "POST" && strings.HasPrefix(entry.Path, "/app/installations/") && strings.HasSuffix(entry.Path, "/access_tokens")) {
			t.Fatal("unexpected GitHub write")
		}
		if strings.HasPrefix(entry.Path, "/repos/KingYoSun/") {
			t.Fatal("outside repository was read")
		}
		raw, _ := json.Marshal(entry)
		t.Log(string(raw))
	}
}

type traceTransport func(*http.Request) (*http.Response, error)

func (f traceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
