package repositories

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func TestRepositoryNavigationPostgres(t *testing.T) {
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
	for _, user := range []string{"alice", "bob", "carol", "outsider"} {
		if _, err = store.Database().Exec(ctx, `INSERT INTO auth_users VALUES($1,'repository-fixture',$1,$1||'@koujou.test',$1,false)`, user); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("k", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), true
	})
	s := New(store, a, []byte(strings.Repeat("k", 32)))
	observer, err := githubobserve.New(ctx, store, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.AttachObserver(observer)
	contextService := contexts.New(store, a, contexts.DefaultLimits())
	backend := publications.NewRemote(nil)
	pub := publications.New(store, a, runs.New(store, a, contextService), backend)
	s.AttachPublication(backend, pub)
	mux := http.NewServeMux()
	a.Register(mux)
	s.Register(mux)
	contextService.Register(mux)
	observer.Register(mux)
	pub.Register(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	serial := 0
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
			t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, status, value)
		}
		if bytes.Contains(value, []byte("fixture-private-token")) {
			t.Fatal("credential exposed in response")
		}
		return value
	}
	mutation := func(version string) map[string]any {
		serial++
		return map[string]any{"operation_id": fmt.Sprintf("op-%d", serial), "expected_version": version}
	}
	accepted := func(value []byte) string {
		t.Helper()
		var out struct{ Data struct{ Version string } }
		if json.Unmarshal(value, &out) != nil || out.Data.Version == "" {
			t.Fatalf("not accepted: %s", value)
		}
		return out.Data.Version
	}
	for _, pair := range [][2]string{{"alice", "one"}, {"outsider", "two"}} {
		body := mutation("0")
		body["id"] = pair[1]
		body["name"] = pair[1]
		request(pair[0], "POST", "/access/workspaces/"+pair[1]+"/create", body, 200)
	}
	if _, err = store.Database().Exec(ctx, `INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','bob','editor'),('one','carol','viewer')`); err != nil {
		t.Fatal(err)
	}
	project := mutation("0")
	project["value"] = map[string]any{"kind": "project", "name": "Project"}
	request("alice", "POST", "/contexts/workspaces/one/scope/project", project, 200)
	var creates, remoteCalls atomic.Int64
	var rejectCreates atomic.Bool
	creationStarted, finishCreation := make(chan struct{}, 1), make(chan struct{})
	var mu sync.Mutex
	repos := map[string]remoteRepository{}
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls.Add(1)
		if r.Header.Get("Authorization") != "token fixture-private-token" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/api/v1/user" {
			json.NewEncoder(w).Encode(map[string]any{"id": 7, "login": "owner"})
			return
		}
		if r.URL.Path == "/api/v1/user/orgs" {
			json.NewEncoder(w).Encode([]any{})
			return
		}
		if r.Method == "POST" && (r.URL.Path == "/api/v1/user/repos" || r.URL.Path == "/api/v1/orgs/team/repos") {
			var input struct {
				Name    string `json:"name"`
				Private bool   `json:"private"`
			}
			json.NewDecoder(r.Body).Decode(&input)
			if rejectCreates.Load() {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			owner := "owner"
			if strings.Contains(r.URL.Path, "/orgs/") {
				owner = "team"
			}
			value := remoteRepository{ID: creates.Add(1), FullName: owner + "/" + input.Name, Private: input.Private}
			mu.Lock()
			repos[value.FullName] = value
			mu.Unlock()
			if strings.HasPrefix(input.Name, "lost") {
				connection, _, _ := w.(http.Hijacker).Hijack()
				connection.Close()
				return
			}
			if input.Name == "in-flight" {
				select {
				case creationStarted <- struct{}{}:
				default:
				}
				<-finishCreation
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(value)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/repos/") {
			name := strings.TrimPrefix(r.URL.Path, "/api/v1/repos/")
			mu.Lock()
			value, ok := repos[name]
			mu.Unlock()
			if !ok {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(value)
			return
		}
		w.WriteHeader(404)
	}))
	defer forge.Close()
	base := "/forge/workspaces/one"
	connection := mutation("0")
	connection["name"] = "Shared Gitea"
	connection["instance"] = forge.URL
	connection["token"] = "fixture-private-token"
	connection["allow_create"] = true
	request("bob", "POST", base+"/connections/shared", connection, 403)
	request("carol", "POST", base+"/connections/shared", connection, 403)
	connectionVersion := accepted(request("alice", "POST", base+"/connections/shared", connection, 200))
	request("outsider", "GET", base+"/connections", nil, 403)
	var encrypted []byte
	if err = store.Database().QueryRow(ctx, `SELECT credential FROM forge_connections WHERE workspace_id='one' AND id='shared'`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("fixture-private-token")) {
		t.Fatal("credential stored in plaintext")
	}
	if _, err = s.open("two", "shared", encrypted); err == nil {
		t.Fatal("credential moved across Workspace")
	}
	request("alice", "GET", base+"/connections/shared/owners", nil, 200)
	create := func(id, name, owner string, private bool) map[string]any {
		body := mutation("0")
		body["connection"] = "shared"
		body["full_name"] = owner + "/" + name
		body["private"] = private
		body["project"] = "project"
		return body
	}
	first := create("first", "first", "owner", true)
	request("bob", "POST", base+"/repositories/first/create", first, 403)
	request("carol", "POST", base+"/repositories/first/create", first, 403)
	request("outsider", "POST", base+"/repositories/first/create", first, 403)
	firstAck := accepted(request("alice", "POST", base+"/repositories/first/create", first, 200))
	if replayAck := accepted(request("alice", "POST", base+"/repositories/first/create", first, 200)); replayAck != firstAck {
		t.Fatal("creation replay changed its acceptance reference")
	}
	if creates.Load() != 1 {
		t.Fatal("create intent dispatched more than once")
	}
	repo, err := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "first")
	if err != nil || repo.State != "created" || !repo.Private || repo.Project != "project" {
		t.Fatalf("created repo not registered: %+v %v", repo, err)
	}
	request("alice", "GET", base+"/repositories?project=project", nil, 200)
	request("bob", "GET", base+"/repositories/first", nil, 200)
	request("outsider", "GET", "/forge/workspaces/two/repositories/first", nil, 403)
	lost := create("lost", "lost", "team", false)
	accepted(request("alice", "POST", base+"/repositories/lost/create", lost, 200))
	accepted(request("alice", "POST", base+"/repositories/lost/create", lost, 200))
	if creates.Load() != 2 {
		t.Fatal("unknown outcome was resent")
	}
	unknown, _ := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "lost")
	if unknown.State != "unknown" {
		t.Fatalf("lost response state: %s", unknown.State)
	}
	accepted(request("alice", "POST", base+"/repositories/lost/reconcile", mutation(unknown.Version), 200))
	confirmed, _ := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "lost")
	if confirmed.State != "confirmed" {
		t.Fatal("explicit reconciliation did not register repo")
	}
	observe := mutation("0")
	observe["kind"] = "actions"
	observe["workflow"] = "build.yml"
	observe["required_jobs"] = []string{"Linux"}
	observationVersion := accepted(request("alice", "POST", base+"/repositories/first/observations/build", observe, 200))
	configs := request("alice", "GET", base+"/repositories/first/observations", nil, 200)
	if !bytes.Contains(configs, []byte("build.yml")) || !bytes.Contains(configs, []byte(observationVersion)) {
		t.Fatal("saved observation configuration cannot be reopened")
	}
	stopped := mutation(observationVersion)
	stopped["kind"] = "actions"
	stopped["workflow"] = "build.yml"
	stopped["required_jobs"] = []string{"Linux"}
	stopped["active"] = false
	accepted(request("alice", "POST", base+"/repositories/first/observations/build", stopped, 200))
	if _, err = githubobserve.New(ctx, store, a, nil); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err = store.Database().QueryRow(ctx, `SELECT active FROM github_targets WHERE id='gitea:build'`).Scan(&active); err != nil || active {
		t.Fatal("observation stop was lost during restart")
	}
	request("bob", "POST", base+"/repositories/first/observations/other", observe, 403)
	catalog := request("bob", "GET", "/publications/workspaces/one/connections", nil, 200)
	if !bytes.Contains(catalog, []byte("owner/first")) {
		t.Fatal("registered repo not offered for publication")
	}
	remote, ok := backend.Connection("one", "repository:first")
	if !ok || remote.RepositoryID != repo.RepositoryID || remote.Resource.ID != "forge-repository:first" {
		t.Fatal("publication binding lost fixed repository identity")
	}
	resourceBase := "/access/workspaces/one/resources/forge-repository:first"
	policy := mutation("0")
	policy["restricted"] = true
	request("bob", "POST", resourceBase, policy, 403)
	policyVersion := accepted(request("alice", "POST", resourceBase, policy, 200))
	request("bob", "GET", base+"/repositories/first", nil, 403)
	hidden := request("bob", "GET", "/publications/workspaces/one/connections", nil, 200)
	if bytes.Contains(hidden, []byte("owner/first")) {
		t.Fatal("restricted repo leaked through connection candidates")
	}
	grant := mutation(policyVersion)
	grant["grant"] = map[string]any{"user": "bob", "read": true, "update": false, "execute": false}
	policyVersion = accepted(request("alice", "POST", resourceBase, grant, 200))
	request("bob", "GET", base+"/repositories/first", nil, 200)
	request("bob", "GET", resourceBase, nil, 403)
	// Unregister without losing identity, existing references or dispatch receipts.
	rejectCreates.Store(true)
	refused := create("rejected", "first", "owner", true)
	accepted(request("alice", "POST", base+"/repositories/rejected/create", refused, 200))
	rejectCreates.Store(false)
	rejected, err := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "rejected")
	if err != nil || rejected.State != "rejected" {
		t.Fatalf("missing refused intent: %+v %v", rejected, err)
	}
	remove := mutation(rejected.Version)
	removePath := base + "/repositories/rejected/delete"
	for _, actor := range []string{"bob", "carol", "outsider"} {
		request(actor, "POST", removePath, remove, 403)
	}
	request("alice", "POST", removePath, mutation("0"), 409)
	restricted := mutation("0")
	restricted["restricted"] = true
	rejectedPolicy := "/access/workspaces/one/resources/forge-repository:rejected"
	policyHead := accepted(request("alice", "POST", rejectedPolicy, restricted, 200))
	readOnly := mutation(policyHead)
	readOnly["grant"] = map[string]any{"user": "alice", "read": true, "update": false}
	policyHead = accepted(request("alice", "POST", rejectedPolicy, readOnly, 200))
	request("alice", "POST", removePath, remove, 403)
	allow := mutation(policyHead)
	allow["grant"] = map[string]any{"user": "alice", "read": true, "update": true}
	accepted(request("alice", "POST", rejectedPolicy, allow, 200))
	beforeRemoval := remoteCalls.Load()
	removed := accepted(request("alice", "POST", removePath, remove, 200))
	if accepted(request("alice", "POST", removePath, remove, 200)) != removed {
		t.Fatal("delete replay changed its receipt")
	}
	listed := request("alice", "GET", base+"/repositories", nil, 200)
	if bytes.Contains(listed, []byte(`"id":"rejected"`)) {
		t.Fatal("removed refused intent remained in the current catalog")
	}
	request("alice", "GET", base+"/repositories/rejected", nil, 200)
	request("bob", "GET", base+"/repositories/first", nil, 200)
	accepted(request("alice", "POST", base+"/repositories/rejected/create", refused, 200))
	request("alice", "POST", base+"/repositories/rejected/create", create("rejected", "first", "owner", true), 409)
	if remoteCalls.Load() != beforeRemoval || creates.Load() != 2 {
		t.Fatal("removal or an old create intent contacted Gitea")
	}
	var deleted bool
	if err = store.Database().QueryRow(ctx, `SELECT deleted FROM shared_versions WHERE workspace_id='one' AND resource_id='forge-repository:rejected'`).Scan(&deleted); err != nil || !deleted {
		t.Fatal("deletion tombstone was not committed")
	}
	var priorRejected int
	if err = store.Database().QueryRow(ctx, `SELECT count(*) FROM shared_changes WHERE workspace_id='one' AND resource_id='forge-repository:rejected' AND payload->>'state'='rejected' AND NOT deleted`).Scan(&priorRejected); err != nil || priorRejected == 0 {
		t.Fatal("refused intent history was erased")
	}
	ownerGrant := mutation(policyVersion)
	ownerGrant["grant"] = map[string]any{"user": "alice", "read": true, "update": true}
	accepted(request("alice", "POST", resourceBase, ownerGrant, 200))
	registered := mutation("0")
	registered["connection"], registered["full_name"], registered["project"] = "shared", "owner/first", "project"
	request("alice", "POST", base+"/repositories/registered/register", registered, 200)
	request("alice", "POST", base+"/repositories/unknown/create", create("unknown", "lost-unregister", "team", true), 200)
	for _, id := range []string{"first", "lost", "registered", "unknown"} {
		prior, err := s.Read(ctx, access.Principal{UserID: "alice"}, "one", id)
		if err != nil {
			t.Fatal(err)
		}
		before := remoteCalls.Load()
		request("alice", "POST", base+"/repositories/"+id+"/delete", mutation(prior.Version), 200)
		retained, err := s.Read(ctx, access.Principal{UserID: "alice"}, "one", id)
		if err != nil || retained.FullName != prior.FullName || retained.RepositoryID != prior.RepositoryID || retained.State != prior.State || remoteCalls.Load() != before {
			t.Fatalf("unregistration changed the referenced repository: %+v %v", retained, err)
		}
		edit := mutation(retained.Version)
		edit["connection"], edit["full_name"], edit["project"] = "shared", prior.FullName, "project"
		if prior.RepositoryID > 0 {
			request("alice", "POST", base+"/repositories/"+id+"/register", edit, 409)
		}
	}
	resolved, _, _, err := s.Resolve(ctx, "one", "first")
	if err != nil || resolved.RepositoryID != repo.RepositoryID || resolved.Version != repo.Version {
		t.Fatal("an existing repository reference stopped resolving")
	}
	if retained, ok := backend.Connection("one", "repository:first"); !ok || retained.Resource != remote.Resource {
		t.Fatal("an existing publication lost its fixed repository reference")
	}
	candidates := request("alice", "GET", "/publications/workspaces/one/connections", nil, 200)
	if bytes.Contains(candidates, []byte("owner/first")) {
		t.Fatal("unregistered repository remained a new publication candidate")
	}
	private := true
	pending := RepositoryWrite{Mutation: apicontract.Mutation{OperationID: "unstarted", ExpectedVersion: "0"}, Connection: "shared", FullName: "owner/pending", Private: &private, Project: "project"}
	pendingRepo := Repository{ID: "pending", Connection: "shared", ConnectionVersion: connectionVersion, Instance: forge.URL, FullName: pending.FullName, Private: true, Project: "project", State: "intent"}
	pendingAck, err := s.saveRepository(ctx, access.Principal{UserID: "alice"}, "one", pendingRepo, pending.Mutation, false, pending)
	if err != nil || pendingAck.Error != nil {
		t.Fatal("pending intent fixture failed", err)
	}
	beforePending := remoteCalls.Load()
	request("alice", "POST", base+"/repositories/pending/delete", mutation(pendingAck.Data.Version), 200)
	request("alice", "POST", base+"/repositories/pending/create", pending, 200)
	if remoteCalls.Load() != beforePending {
		t.Fatal("an unregistered, undispatched intent contacted Gitea")
	}
	var release sync.Once
	defer release.Do(func() { close(finishCreation) })
	inFlight := create("in-flight", "in-flight", "owner", true)
	result, failed := make(chan *http.Response, 1), make(chan error, 1)
	go func() {
		req, _ := http.NewRequest("POST", api.URL+base+"/repositories/in-flight/create", bytes.NewReader(raw(inFlight)))
		req.Header.Set("Authorization", "Bearer alice")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			failed <- err
		} else {
			result <- res
		}
	}()
	select {
	case <-creationStarted:
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight creation did not reach owned Gitea")
	}
	activeIntent, _ := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "in-flight")
	request("alice", "POST", base+"/repositories/in-flight/delete", mutation(activeIntent.Version), 200)
	release.Do(func() { close(finishCreation) })
	select {
	case res := <-result:
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("in-flight creation response: %d %s", res.StatusCode, body)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight creation did not finish")
	}
	archivedResult, _ := s.Read(ctx, access.Principal{UserID: "alice"}, "one", "in-flight")
	if archivedResult.State != "created" || archivedResult.RepositoryID == 0 {
		t.Fatal("the result of an unregistered in-flight request was lost")
	}
	if err = store.Database().QueryRow(ctx, `SELECT deleted FROM shared_versions WHERE workspace_id='one' AND resource_id='forge-repository:in-flight'`).Scan(&deleted); err != nil || !deleted {
		t.Fatal("a late creation result registered the repository again")
	}
	listed = request("alice", "GET", base+"/repositories", nil, 200)
	var page Page[Repository]
	json.Unmarshal(listed, &page)
	if len(page.Items) != 0 {
		t.Fatal("unregistered repositories remained in the current catalog")
	}
	for i := range 52 {
		id := fmt.Sprintf("page-%02d", i)
		r := Repository{ID: id, Connection: "shared", Instance: forge.URL, FullName: "owner/" + id, RepositoryID: int64(100 + i), Private: true, State: "registered"}
		if out, err := s.saveRepository(ctx, access.Principal{UserID: "alice"}, "one", r, apicontract.Mutation{OperationID: id, ExpectedVersion: "0"}, false, r); err != nil || out.Error != nil {
			t.Fatal("catalog fixture failed", err)
		}
	}
	json.Unmarshal(request("alice", "GET", base+"/repositories", nil, 200), &page)
	if len(page.Items) != 50 || page.Next != "page-49" {
		t.Fatalf("first active catalog page: %d %s", len(page.Items), page.Next)
	}
	var next Page[Repository]
	json.Unmarshal(request("alice", "GET", base+"/repositories?after="+page.Next, nil, 200), &next)
	if len(next.Items) != 2 || next.Items[0].ID != "page-50" || next.Next != "" {
		t.Fatal("active catalog pagination lost or repeated records")
	}
	beforeRevocation := creates.Load()
	revoked := mutation(connectionVersion)
	revoked["name"] = "Shared Gitea"
	revoked["instance"] = forge.URL
	revoked["allow_create"] = false
	accepted(request("alice", "POST", base+"/connections/shared", revoked, 200))
	request("alice", "POST", base+"/repositories/denied/create", create("denied", "denied", "owner", true), 400)
	if creates.Load() != beforeRevocation {
		t.Fatal("revoked create permission dispatched")
	}
	var history string
	if err = store.Database().QueryRow(ctx, `SELECT string_agg(payload::text,'') FROM shared_changes WHERE workspace_id='one'`).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history, "fixture-private-token") {
		t.Fatal("credential reached shared history")
	}
	t.Log("Workspace admin credentials, explicit owner/privacy, one dispatch, unknown reconciliation, Project association, observation/publication setup and resource grants passed")
}
