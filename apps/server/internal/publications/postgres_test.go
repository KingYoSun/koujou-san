package publications

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	mu     sync.Mutex
	issues []Issue
	posts  int
	mode   string
}
type crashBackend struct{ Backend }

func (b crashBackend) Create(ctx context.Context, c Connection, d Draft, body string) Outcome {
	_ = b.Backend.Create(ctx, c, d, body)
	panic("owned fixture: process lost after external creation")
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/app/installations/") {
		var request struct {
			Repositories []int64           `json:"repository_ids"`
			Permissions  map[string]string `json:"permissions"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Repositories) != 1 || request.Repositories[0] != 8 || len(request.Permissions) != 2 || request.Permissions["issues"] != "write" || request.Permissions["metadata"] != "read" {
			http.Error(w, "scope", 403)
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "fixture-token", "expires_at": time.Now().Add(time.Hour), "permissions": request.Permissions, "repositories": []any{map[string]any{"id": 8, "full_name": "fixture/repo"}}})
		return
	}
	if r.Header.Get("Authorization") != "Bearer fixture-token" {
		http.Error(w, "auth", 401)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/repos/fixture/repo/issues" {
		f.posts++
		if f.mode == "reject" {
			http.Error(w, "invalid input", 422)
			return
		}
		var d Draft
		if json.NewDecoder(r.Body).Decode(&d) != nil {
			http.Error(w, "body", 400)
			return
		}
		n := int64(len(f.issues) + 1)
		issue := Issue{ID: n + 100, Number: n, URL: fmt.Sprintf("https://github.com/fixture/repo/issues/%d", n), Title: d.Title, Body: d.Body, State: "open", UpdatedAt: time.Now().UTC()}
		f.issues = append(f.issues, issue)
		if f.mode == "lost" {
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(issue)
		return
	}
	if r.URL.Path == "/search/issues" {
		items := []Issue{}
		query := r.URL.Query().Get("q")
		if f.mode != "not-indexed" {
			for _, issue := range f.issues {
				digest := strings.TrimPrefix(query, "repo:fixture/repo is:issue in:body ")
				if strings.Contains(issue.Body, digest) {
					items = append(items, issue)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(items), "incomplete_results": false, "items": items})
		return
	}
	var number int64
	if r.Method == "GET" {
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/fixture/repo/issues/%d", &number); err == nil && number > 0 && int(number) <= len(f.issues) {
			_ = json.NewEncoder(w).Encode(f.issues[number-1])
			return
		}
	}
	http.NotFound(w, r)
}
func TestPublicationsPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:publications")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.Database().Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []string{"admin", "editor", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, u)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider'); INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','editor','editor',false),('one','viewer','viewer',false),('two','outsider','admin',true)`)
	a := access.New(store, []byte(strings.Repeat("p", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		user := r.Header.Get("X-Fixture-User")
		return user, user != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	rs := runs.New(store, a, c)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fixture{}
	httpServer := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer httpServer.Close()
	remote := NewRemote(&Config{AppID: "fixture-app", key: key, Connections: []Connection{{ID: "fixture", Workspace: "one", Repository: "fixture/repo", RepositoryID: 8, InstallationID: 7}}})
	remote.base = httpServer.URL
	s := New(store, a, rs, remote)
	admin := access.Principal{UserID: "admin"}
	editor := access.Principal{UserID: "editor"}
	viewer := access.Principal{UserID: "viewer"}
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("publication-%d", serial), ExpectedVersion: version}
	}
	accept := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		ref, e := accepted(v, err)
		if e != nil {
			t.Fatalf("response %+v %v", v, err)
		}
		return ref
	}
	draft := Draft{Name: "fixed draft", Connection: "fixture", Title: "fixed title", Body: "original user body", Sources: []Ref{}}
	dr := accept(s.Put(ctx, editor, "one", "publication-draft:one", Write{Mutation: mutation("0"), Value: raw(draft)}))
	count := func() int { fake.mu.Lock(); defer fake.mu.Unlock(); return fake.posts }
	current := func(id string) (Request, Record) {
		t.Helper()
		rec, err := s.Read(ctx, admin, "one", Ref{ID: id, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var r Request
		if decode(rec.Value, &r) != nil {
			t.Fatal("request decode")
		}
		return r, rec
	}
	submit := func(id string) Ref {
		t.Helper()
		return accept(s.Submit(ctx, admin, "one", id, Submit{Mutation: mutation("0"), Draft: dr}))
	}
	run := func(id string) {
		t.Helper()
		if err := s.Process(ctx, "one", id); err != nil {
			t.Fatal("process", id, err)
		}
	}
	if count() != 0 {
		t.Fatal("save sent externally")
	}
	for _, p := range []access.Principal{editor, viewer, {UserID: "outsider"}} {
		if v, err := s.Submit(ctx, p, "one", "publication:denied-"+p.UserID, Submit{Mutation: mutation("0"), Draft: dr}); err == nil && v.Error == nil {
			t.Fatal("unauthorized submit", p)
		}
	}
	preview, err := s.Preview(ctx, admin, "one", "publication:first", dr)
	if err != nil || !strings.HasPrefix(preview.Body, draft.Body) || !strings.Contains(preview.Body, "<!-- koujou-san-request:") || strings.Contains(preview.Marker, "one") {
		t.Fatal("opaque preview", preview, err)
	}
	req := Submit{Mutation: mutation("0"), Draft: dr}
	first := accept(s.Submit(ctx, admin, "one", "publication:first", req))
	if replay := accept(s.Submit(ctx, admin, "one", "publication:first", req)); replay != first {
		t.Fatal("receipt changed")
	}
	initial, _ := current(first.ID)
	if initial.State != "accepted" || initial.External != nil || count() != 0 {
		t.Fatal("acceptance became creation")
	}
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- s.Process(ctx, "one", first.ID) }()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil && !errorsIsState(err) {
			t.Fatal(err)
		}
	}
	confirmed, rec := current(first.ID)
	if confirmed.State != "created" || confirmed.External == nil || count() != 1 || confirmed.External.Body != preview.Body {
		t.Fatal("confirmed result", confirmed, count())
	}
	original, _ := s.Read(ctx, admin, "one", first)
	if !strings.Contains(string(original.Value), `"accepted"`) {
		t.Fatal("accepted input overwritten")
	}
	runRecord, err := rs.Read(ctx, viewer, "one", Ref{ID: confirmed.Run, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	var recorded runs.Run
	_ = json.Unmarshal(runRecord.Value, &recorded)
	if recorded.State != "succeeded" || recorded.Execution.Environment != "server-connector" || recorded.Assignment != nil {
		t.Fatal("fabricated CLI", recorded)
	}
	// External canonical content may change without a new create/update command.
	fake.mu.Lock()
	fake.issues[0].Title = "manually edited"
	fake.issues[0].Body = "external canonical body"
	fake.mu.Unlock()
	observed, err := s.Refresh(ctx, viewer, "one", rec.Ref)
	if err != nil {
		t.Fatal(err)
	}
	var edited Request
	_ = json.Unmarshal(observed.Value, &edited)
	if edited.External.Title != "manually edited" || edited.Input.Title != draft.Title || count() != 1 {
		t.Fatal("external edit overwritten")
	}
	fake.mu.Lock()
	fake.mode = "lost"
	fake.mu.Unlock()
	lost := submit("publication:lost")
	run(lost.ID)
	unknown, unknownRec := current(lost.ID)
	if unknown.State != "unknown" || count() != 2 {
		t.Fatal("response loss treated as failure", unknown)
	}
	fake.mu.Lock()
	fake.mode = "not-indexed"
	fake.mu.Unlock()
	if _, err = s.Refresh(ctx, admin, "one", unknownRec.Ref); err != nil {
		t.Fatal(err)
	}
	unknown, unknownRec = current(lost.ID)
	if unknown.State != "unknown" || count() != 2 {
		t.Fatal("empty lookup caused retry")
	}
	fake.mu.Lock()
	fake.mode = ""
	fake.mu.Unlock()
	if _, err = s.Refresh(ctx, admin, "one", unknownRec.Ref); err != nil {
		t.Fatal(err)
	}
	recovered, _ := current(lost.ID)
	if recovered.State != "created" || recovered.External.Number != 2 || count() != 2 {
		t.Fatal("marker reconciliation", recovered)
	}
	fake.mu.Lock()
	fake.mode = "reject"
	fake.mu.Unlock()
	rejected := submit("publication:rejected")
	run(rejected.ID)
	failed, _ := current(rejected.ID)
	if failed.State != "failed" || failed.Reason != "creation_rejected_422" || failed.ObservedAt == nil {
		t.Fatal("confirmed rejection", failed)
	}
	before := count()
	run(rejected.ID)
	if count() != before {
		t.Fatal("failed request auto-retried")
	}
	fake.mu.Lock()
	fake.mode = ""
	fake.mu.Unlock()
	// A current shared edit or revoked permission prevents sending accepted input.
	held := submit("publication:held")
	changed := draft
	changed.Title = "new adopted content"
	dr2 := accept(s.Put(ctx, editor, "one", dr.ID, Write{Mutation: mutation(dr.Version), Value: raw(changed)}))
	run(held.ID)
	heldState, _ := current(held.ID)
	if heldState.State != "held" || count() != before {
		t.Fatal("stale draft sent")
	}
	dr = accept(s.Put(ctx, editor, "one", dr.ID, Write{Mutation: mutation(dr2.Version), Value: raw(draft)}))
	cancelled := submit("publication:revoked")
	exec(`UPDATE access_members SET can_execute=false WHERE workspace_id='one' AND user_id='admin'`)
	run(cancelled.ID)
	if count() != before {
		t.Fatal("revoked actor sent")
	}
	exec(`UPDATE access_members SET can_execute=true WHERE workspace_id='one' AND user_id='admin'`)
	// The internal automation owner uses the same fixed draft and submit path.
	auto := automation.New(store, a)
	control := automation.Control{Kind: "hopper", Enabled: true, CanRead: true, CanCommit: true, Slots: 1, MaxAttempts: 2, LeaseSeconds: 60, TimeoutSeconds: 30, RetrySeconds: 1, MaxRequests: 10, PeriodSeconds: 60}
	cr := accept(auto.Put(ctx, admin, "one", "control:hopper", automation.Write{Mutation: mutation("0"), Value: raw(control)}))
	ar := accept(s.SubmitAutomation(ctx, "one", "publication:automation", Submit{Mutation: mutation("0"), Draft: dr}, cr))
	run(ar.ID)
	automatic, _ := current(ar.ID)
	if automatic.State != "created" || automatic.Actor.Kind != "automation" || automatic.Actor.Control == nil {
		t.Fatal("automation path", automatic)
	}
	disabled := accept(s.SubmitAutomation(ctx, "one", "publication:automation-cancelled", Submit{Mutation: mutation("0"), Draft: dr}, cr))
	control.Enabled = false
	accept(auto.Put(ctx, admin, "one", cr.ID, automation.Write{Mutation: mutation(cr.Version), Value: raw(control)}))
	before = count()
	run(disabled.ID)
	if count() != before {
		t.Fatal("cancelled automation sent")
	}
	forbidden := draft
	forbidden.Connection = "not-allowed"
	bad := accept(s.Put(ctx, editor, "one", "publication-draft:outside", Write{Mutation: mutation("0"), Value: raw(forbidden)}))
	if v, err := s.Submit(ctx, admin, "one", "publication:outside", Submit{Mutation: mutation("0"), Draft: bad}); err == nil && v.Error == nil {
		t.Fatal("outside repository accepted")
	}
	if count() != before {
		t.Fatal("outside write")
	}
	// Lose the entire sending process after the external side effect, before its
	// result is committed. A replacement repairs the same Run and only observes.
	crash := submit("publication:crashed")
	before = count()
	s = New(store, a, rs, crashBackend{remote})
	crashed := false
	func() {
		defer func() {
			if recover() != nil {
				crashed = true
			}
		}()
		_ = s.Process(ctx, "one", crash.ID)
	}()
	if !crashed || count() != before+1 {
		t.Fatal("fault did not occur after creation")
	}
	exec(`UPDATE publication_queue SET due_at=now()-interval '1 second' WHERE workspace_id='one' AND id=$1`, crash.ID)
	s = New(store, a, rs, remote)
	run(crash.ID)
	abandoned, abandonedRec := current(crash.ID)
	if abandoned.State != "unknown" || count() != before+1 {
		t.Fatal("restart sent again", abandoned)
	}
	if _, err = s.Refresh(ctx, admin, "one", abandonedRec.Ref); err != nil {
		t.Fatal(err)
	}
	repaired, _ := current(crash.ID)
	if repaired.State != "created" || count() != before+1 {
		t.Fatal("restart did not reconcile", repaired)
	}
	page, err := s.Query(ctx, viewer, "one", Query{Kind: "heads", Type: "request", Limit: 1})
	if err != nil || page.Data == nil || len(page.Data.Items) != 1 || page.Data.Next == "" || strings.Contains(string(page.Data.Items[0].Value), "external_body") {
		t.Fatal("metadata page", page, err)
	}
	history, err := s.Query(ctx, viewer, "one", Query{Kind: "history", ID: dr.ID, Limit: 1})
	if err != nil || history.Data.Next == "" {
		t.Fatal("history", history, err)
	}
	next, err := s.Query(ctx, viewer, "one", Query{Kind: "history", ID: dr.ID, After: history.Data.Next, Through: history.Data.Through, Limit: 1})
	if err != nil || next.Data.Items[0].Ref == history.Data.Items[0].Ref {
		t.Fatal("cursor", next, err)
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, dr.ID)
	if _, err = s.Read(ctx, viewer, "one", first); err == nil {
		t.Fatal("request leaked restricted draft")
	}
	if _, err = rs.Read(ctx, viewer, "one", runRecord.Ref); err == nil {
		t.Fatal("common Run leaked restricted draft")
	}
	t.Log("fixed shared acceptance, one concurrent send, App token scope, GET-confirmed creation, lost-response and process-loss reconciliation, canonical edits, definite failure, current authority and bounded pages passed")
}
func errorsIsState(err error) bool { return err == ErrState }
