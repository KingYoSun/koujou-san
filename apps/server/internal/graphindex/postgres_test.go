package graphindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ArcadeData/arcadedb-drivers/go/arcadedb"
	"github.com/ArcadeData/arcadedb-drivers/go/arcadedb/generated"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func TestGraphPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:graph")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, user := range []string{"alice", "bob", "outsider"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO auth_users VALUES($1,'graph-fixture',$1,$1||'@koujou.test',$1,false)", user); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("g", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if id != "alice" && id != "bob" && id != "outsider" {
			w.WriteHeader(401)
			return "", false
		}
		return id, true
	})
	for _, w := range []string{"one", "two"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO access_workspaces VALUES($1,$1,'alice');", w); err != nil {
			t.Fatal(err)
		}
		if _, err = store.Database().Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES($1,'alice','admin')", w); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.Database().Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','bob','viewer')"); err != nil {
		t.Fatal(err)
	}
	c := contexts.New(store, a, contexts.DefaultLimits())
	fault := &graphFault{}
	proxy := fault.proxy(t, os.Getenv("KOUJOU_TEST_GRAPH_URL"))
	defer proxy.Close()
	credential := os.Getenv("KOUJOU_TEST_GRAPH_PASSWORD")
	remote, err := NewRemote(proxy.URL, "root", credential, "koujou_graph_test")
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, a, c, remote, DefaultLimits(), []byte(strings.Repeat("e", 32)))
	defer s.Close()
	n := 0
	mutation := func(v string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: fmt.Sprintf("graph-%d", n), ExpectedVersion: v}
	}
	put := func(kind, id, version string, value any) contexts.Ref {
		t.Helper()
		result, err := c.Put(ctx, access.Principal{UserID: "alice"}, "one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: mutation(version), Value: encode(value)})
		if err != nil || result.Data == nil {
			t.Fatalf("put %s: %v %+v", id, err, result)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: result.Data.Version}
	}
	text := "共有する文"
	sentence := put("sentence", "s", "0", contexts.Unit{Kind: "sentence", Text: &text})
	paragraph := put("paragraph", "p", "0", contexts.Unit{Kind: "paragraph", Children: []contexts.Ref{sentence, sentence}})
	section := put("section", "c", "0", contexts.Unit{Kind: "section", Children: []contexts.Ref{paragraph}})
	document := put("document", "d", "0", contexts.Unit{Kind: "document", Children: []contexts.Ref{section}})
	scope1 := put("scope", "scope1", "0", contexts.Scope{Kind: "issue", Name: "one"})
	scope2 := put("scope", "scope2", "0", contexts.Scope{Kind: "project", Name: "two"})
	for i, scope := range []contexts.Ref{scope1, scope2} {
		put("membership", fmt.Sprintf("m%d", i), "0", contexts.Membership{Scope: contexts.Identity{Kind: []string{"issue", "project"}[i], ID: scope.ID}, Context: contexts.Identity{Kind: sentence.Kind, ID: sentence.ID}, Present: true})
	}
	run, err := store.Apply(ctx, persistence.Scope{ActorID: "fixture", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: "run:declared", Kind: "run", Mutation: mutation("0"), Payload: encode(map[string]string{"kind": "declared-fixture"})}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		if err := a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: ref.ID}, nil); err != nil {
			return nil, err
		}
		return encode(map[string]string{"kind": "declared-fixture"}), nil
	})
	if err != nil || run.Data == nil {
		t.Fatalf("run fixture: %v", err)
	}
	useWrite := RelationWrite{OperationID: mutation("0").OperationID, ExpectedVersion: "0", Value: Relation{Kind: "uses", From: contexts.Ref{Kind: "run", ID: "declared", Revision: run.Data.Version}, To: document, Present: true}}
	relation, err := s.PutRelation(ctx, access.Principal{UserID: "alice"}, "one", "use", useWrite)
	if err != nil || relation.Data == nil {
		t.Fatalf("relation: %v %+v", err, relation)
	}
	generatedRelation, err := s.PutRelation(ctx, access.Principal{UserID: "alice"}, "one", "generation", RelationWrite{OperationID: mutation("0").OperationID, ExpectedVersion: "0", Value: Relation{Kind: "generated", From: contexts.Ref{Kind: "run", ID: "declared", Revision: run.Data.Version}, To: sentence, Present: true}})
	if err != nil || generatedRelation.Data == nil {
		t.Fatal("generation fixture", err)
	}
	if err = s.init(ctx); err != nil {
		t.Fatalf("init: %#v", err)
	}
	if err = s.Rebuild(ctx, "one"); err != nil {
		t.Fatalf("rebuild: %#v", err)
	}
	h, err := remote.Active(ctx, "one")
	if err != nil || h.Active == "" {
		t.Fatal("no active generation")
	}
	row, err := read(ctx, remote.DB, "SELECT kind,ord,source FROM KoujouEdge WHERE g=:g AND kind='contains' ORDER BY ord", map[string]any{"g": h.Active}, 100)
	if err != nil || row.Reason != "" || len(row.Rows) != 4 {
		t.Fatalf("containment: %v %+v", err, row)
	}
	queried, err := s.Query(ctx, access.Principal{UserID: "alice"}, "one", Request{Root: document, Depth: 8})
	if err != nil || queried.Data == nil || queried.Meta.Completeness.State != "complete" {
		t.Fatalf("query: %#v %+v", err, queried)
	}
	nodes, edges := 0, 0
	for _, item := range queried.Data.Items {
		if item.Node != nil {
			nodes++
		}
		if item.Edge != nil {
			edges++
		}
	}
	if nodes != 14 || edges != 15 {
		t.Fatalf("exploration nodes=%d edges=%d: %+v", nodes, edges, queried.Data)
	}
	t.Log("AC-1: real ArcadeDB projection contains fixed versions, ordered shared child, two independent Scope memberships and declared Run use")
	otherText := "other Workspace only"
	other, err := c.Put(ctx, access.Principal{UserID: "alice"}, "two", sentence.Identity(), contexts.Write{Mutation: mutation("0"), Value: encode(contexts.Unit{Kind: "sentence", Text: &otherText})})
	if err != nil || other.Data == nil {
		t.Fatal("other Workspace fixture", err)
	}
	otherRef := contexts.Ref{Kind: "sentence", ID: "s", Revision: other.Data.Version}
	if err = s.Rebuild(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	isolated, err := s.Query(ctx, access.Principal{UserID: "alice"}, "one", Request{Root: otherRef})
	if err != nil || isolated.Data == nil || len(isolated.Data.Items) != 0 || isolated.Meta.Completeness.State != "pending" {
		t.Fatal("cross-Workspace fixed version leaked", err, isolated)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	request := func(user, method, path string, value any, status int) []byte {
		t.Helper()
		r, _ := http.NewRequest(method, api.URL+path, bytes.NewReader(encode(value)))
		r.Header.Set("Authorization", "Bearer "+user)
		resp, err := api.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var raw json.RawMessage
		if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("HTTP %s status %d: %s", path, resp.StatusCode, raw)
		}
		return raw
	}
	query := func(user string, r Request) apicontract.Response[Result] {
		t.Helper()
		var result apicontract.Response[Result]
		raw := request(user, "POST", "/graph/workspaces/one/query", r, 200)
		if json.Unmarshal(raw, &result) != nil || result.Data == nil {
			t.Fatalf("query: %s", raw)
		}
		return result
	}
	// Bounded pages contain only public graph keys, never ArcadeDB record IDs.
	page := query("alice", Request{Root: document, Limit: 3})
	seen := map[string]bool{}
	total := 0
	for {
		for _, item := range page.Data.Items {
			k := string(encode(item))
			if seen[k] {
				t.Fatal("duplicate page item")
			}
			seen[k] = true
			total++
			if strings.Contains(k, "@rid") {
				t.Fatal("RID exposed")
			}
		}
		if page.Meta.NextCursor == nil {
			break
		}
		page = query("alice", Request{Root: document, Limit: 3, Cursor: *page.Meta.NextCursor})
	}
	if total != 29 {
		t.Fatalf("pagination items=%d", total)
	}
	newerRun, err := store.Apply(ctx, persistence.Scope{ActorID: "fixture", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: "run:declared", Kind: "run", Mutation: mutation(run.Data.Version), Payload: encode(map[string]string{"kind": "declared-fixture-v2"})}, func(pgx.Tx, apicontract.Reference) (json.RawMessage, error) {
		return encode(map[string]string{"kind": "declared-fixture-v2"}), nil
	})
	if err != nil || newerRun.Data == nil {
		t.Fatal("Run revision fixture", err)
	}
	replay, err := s.PutRelation(ctx, access.Principal{UserID: "alice"}, "one", "use", useWrite)
	if err != nil || !bytes.Equal(encode(replay.Data), encode(relation.Data)) {
		t.Fatal("fixed Run relation replay changed", err)
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	pastRun := query("alice", Request{Root: useWrite.Value.From, Kinds: []string{"uses"}, Direction: "out"})
	if len(pastRun.Data.Items) != 3 || pastRun.Meta.Completeness.State != "complete" {
		t.Fatal("past Run provenance lost", pastRun)
	}
	selection := ResolveRequest{Graph: Request{Root: document}, Resolve: contexts.ResolveRequest{RunID: "graph-run", Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{sentence}}}, ID: "graph-manifest", Mutation: func() *apicontract.Mutation { m := mutation("0"); return &m }()}
	raw := request("alice", "POST", "/graph/workspaces/one/resolve", selection, 200)
	var saved apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(raw, &saved)
	if saved.Data == nil {
		t.Fatalf("Manifest: %s", raw)
	}
	manifestRef := contexts.Ref{Kind: "manifest", ID: selection.ID, Revision: saved.Data.Version}
	before, err := c.Read(ctx, access.Principal{UserID: "alice"}, "one", manifestRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	rejected := func(r Request, reason string) {
		t.Helper()
		selection.Graph = r
		selection.ID = "blocked-" + reason
		selection.Mutation = func() *apicontract.Mutation { m := mutation("0"); return &m }()
		raw := request("alice", "POST", "/graph/workspaces/one/resolve", selection, 200)
		var result apicontract.Response[contexts.Resolution]
		if json.Unmarshal(raw, &result) != nil || result.Data == nil || result.Data.Ready || len(result.Data.Missing) == 0 || !strings.Contains(result.Data.Missing[0].Source, reason) {
			t.Fatalf("accepted incomplete %s: %s", reason, raw)
		}
		var count int
		_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM context_records WHERE kind='manifest'").Scan(&count)
		if count != 1 {
			t.Fatal("incomplete exploration saved Manifest")
		}
	}
	for _, test := range []struct {
		r      Request
		reason string
	}{{Request{Root: document, Depth: 1}, "depth_limit"}, {Request{Root: document, Nodes: 2}, "node_limit"}, {Request{Root: document, Edges: 1}, "edge_limit"}, {Request{Root: document, Limit: 1}, "page_limit"}} {
		rejected(test.r, test.reason)
	}
	for _, mode := range []string{"truncated", "cut"} {
		fault.arm(mode)
		reason := mode
		if mode == "cut" {
			reason = "stream_incomplete"
		}
		rejected(Request{Root: document}, reason)
	}
	oldTimeout := s.Limits.Timeout
	s.Limits.Timeout = time.Nanosecond
	rejected(Request{Root: document}, "timeout")
	s.Limits.Timeout = oldTimeout
	text2 := "更新した共有文"
	newSentence := put("sentence", "s", sentence.Revision, contexts.Unit{Kind: "sentence", Text: &text2})
	rejected(Request{Root: document}, "projection_pending")
	// Lose a real successful commit response, then reconcile its durable position.
	fault.arm("lost_commit")
	if err = s.Sync(ctx); err != nil {
		t.Fatalf("commit response recovery: %#v", err)
	}
	current, err := remote.Active(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	cpHeader, err := headerRead(ctx, remote.DB, checkpointKey(current.Active))
	if err != nil {
		t.Fatal(err)
	}
	cp, err := parseCheckpoint(cpHeader)
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Commit(ctx, "one", current.Active, batch{}, cp, cp); err != nil {
		t.Fatal("replay not idempotent", err)
	}
	if err = remote.Commit(ctx, "one", current.Active, batch{}, checkpoint{0, "old"}, checkpoint{1, "older"}); err == nil {
		t.Fatal("old event accepted")
	}
	versionNodes := query("alice", Request{Root: contexts.Ref{Kind: "sentence", ID: "s"}, Kinds: []string{"version"}})
	versions := 0
	for _, item := range versionNodes.Data.Items {
		if item.Node != nil && item.Node.Form == "revision" {
			versions++
			if item.Node.Ref() != newSentence {
				t.Fatal("old current version")
			}
		}
	}
	if versions != 1 {
		t.Fatal("latest version not unique")
	}
	// Changes of the same relationship in one replay batch converge to its final state.
	var membership0 string
	_ = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='one' AND resource_id='membership:m0'").Scan(&membership0)
	m0 := put("membership", "m0", membership0, contexts.Membership{Scope: contexts.Identity{Kind: "issue", ID: scope1.ID}, Context: sentence.Identity(), Present: true})
	put("membership", "m0", m0.Revision, contexts.Membership{Scope: contexts.Identity{Kind: "issue", ID: scope1.ID}, Context: sentence.Identity(), Present: false})

	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := query("alice", Request{Root: contexts.Ref{Kind: "scope", ID: scope1.ID}, Kinds: []string{"membership"}}); len(got.Data.Items) != 1 {
		t.Fatal("removed membership retained")
	}
	t.Log("AC-2/3: public pagination, explicit graph-to-Manifest, limits, truncated/cut streams, timeout/lag rejection, committed-response reconciliation, replay/old position and membership removal passed")
	// Current authorization prunes both endpoints and the relationship owner.
	if err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: "context:s", Restricted: true}, []access.ResourceGrant{{UserID: "alice", Read: true, Update: true}})
	}); err != nil {
		t.Fatal(err)
	}
	bob := query("bob", Request{Root: document})
	for _, item := range bob.Data.Items {
		if item.Node != nil && item.Node.ID == "s" {
			t.Fatal("restricted child leaked")
		}
		if item.Edge != nil && (item.Edge.From.ID == "s" || item.Edge.To.ID == "s") {
			t.Fatal("restricted edge leaked")
		}
	}
	request("bob", "POST", "/graph/workspaces/two/query", Request{Root: document}, 403)
	request("outsider", "POST", "/graph/workspaces/one/query", Request{Root: document}, 403)
	page = query("alice", Request{Root: document, Limit: 1})
	request("bob", "POST", "/graph/workspaces/one/query", Request{Root: document, Limit: 1, Cursor: *page.Meta.NextCursor}, 400)
	if _, err = store.Database().Exec(ctx, "UPDATE access_members SET active=false WHERE workspace_id='one' AND user_id='bob'"); err != nil {
		t.Fatal(err)
	}
	request("bob", "POST", "/graph/workspaces/one/query", Request{Root: document}, 403)
	after, err := c.Read(ctx, access.Principal{UserID: "alice"}, "one", manifestRef)
	if err != nil || !bytes.Equal(before.Value, after.Value) {
		t.Fatal("fixed Manifest changed")
	}
	t.Log("AC-5: current restricted-child policy, cross-Workspace/user traversal, actor-bound cursor and membership revocation passed; fixed Manifest unchanged")
	// Enough immutable records to force multiple snapshot commits.
	for i := 0; i < 520; i++ {
		put("sentence", fmt.Sprintf("bulk-%04d", i), "0", contexts.Unit{Kind: "sentence", Text: &text})
	}
	prior, _ := remote.Active(ctx, "one")
	fault.arm("fail_second_batch")
	if err = s.Rebuild(ctx, "one"); err == nil {
		t.Fatal("snapshot interruption not injected")
	}
	progress, err := read(ctx, remote.DB, "SELECT loaded_nodes,loaded_edges FROM KoujouHeader WHERE w=:w AND stage_status='loading'", map[string]any{"w": "one"}, -1)
	if err != nil || progress.Reason != "" || len(progress.Rows) != 1 || progress.Rows[0]["loaded_nodes"].(float64) < 500 {
		t.Fatal("snapshot progress not durable", err, progress)
	}
	unchanged, _ := remote.Active(ctx, "one")
	if unchanged.Active != prior.Active {
		t.Fatal("incomplete snapshot activated")
	}
	fault.mu.Lock()
	fault.reached = make(chan struct{})
	fault.release = make(chan struct{})
	fault.mu.Unlock()
	fault.arm("pause_snapshot")
	done := make(chan error, 1)
	go func() { done <- s.Rebuild(ctx, "one") }()
	select {
	case <-fault.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("snapshot boundary not reached")
	}
	latestText := "snapshotと並行した更新"
	latest := put("sentence", "s", newSentence.Revision, contexts.Unit{Kind: "sentence", Text: &latestText})
	var membershipVersion string
	_ = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='one' AND resource_id='membership:m1'").Scan(&membershipVersion)
	put("membership", "m1", membershipVersion, contexts.Membership{Scope: contexts.Identity{Kind: "project", ID: scope2.ID}, Context: sentence.Identity(), Present: false})
	added, err := s.PutRelation(ctx, access.Principal{UserID: "alice"}, "one", "concurrent", RelationWrite{OperationID: mutation("0").OperationID, ExpectedVersion: "0", Value: Relation{Kind: "reference", From: document, To: latest, Present: true, Origin: &contexts.Origin{Kind: "adopted-ai", ID: "proposal", Revision: "fixed-proposal", Digest: strings.Repeat("a", 64)}}})
	if err != nil || added.Data == nil {
		t.Fatal("concurrent relation", err)
	}
	close(fault.release)
	if err = <-done; err != nil {
		t.Fatalf("concurrent rebuild: %#v", err)
	}
	if got := query("alice", Request{Root: latest, Kinds: []string{"reference"}}); got.Meta.Completeness.State != "complete" || len(got.Data.Items) != 3 {
		t.Fatalf("concurrent relation lost: %+v", got)
	} else {
		for _, item := range got.Data.Items {
			if item.Edge != nil && (item.Edge.Source.Revision != added.Data.Version || item.Edge.Origin == nil || item.Edge.Origin.Revision != "fixed-proposal") {
				t.Fatal("adopted source provenance changed")
			}
		}
	}
	if got := query("alice", Request{Root: contexts.Ref{Kind: "scope", ID: scope2.ID}, Kinds: []string{"membership"}}); len(got.Data.Items) != 1 {
		t.Fatal("concurrent membership lost")
	}
	// Roll back the real write transaction after data commands but before its
	// checkpoint, then retry; data and position must become visible together.
	beforeCP, _ := remote.Active(ctx, "one")
	beforeHeader, _ := headerRead(ctx, remote.DB, checkpointKey(beforeCP.Active))
	latestText = "commit前の応答喪失"
	latest = put("sentence", "s", latest.Revision, contexts.Unit{Kind: "sentence", Text: &latestText})
	fault.arm("before_checkpoint")
	if err = s.Sync(ctx); err == nil {
		t.Fatal("pre-commit failure not injected")
	}
	afterHeader, _ := headerRead(ctx, remote.DB, checkpointKey(beforeCP.Active))
	if afterHeader != beforeHeader {
		t.Fatal("failed batch advanced checkpoint")
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// A native backup restores the same stable IDs, versions, relations and marker
	// into a distinct, disposable database; native RIDs are never the API identity.
	backupView := query("alice", Request{Root: document})
	backup, err := remote.DB.Command(ctx, arcadedb.SQL, "BACKUP DATABASE 'graph-fixture.zip'", nil)
	if err != nil {
		t.Fatalf("backup: %#v", err)
	}
	t.Logf("backup fixture result: %+v", backup.Result)
	response, err := remote.Server.Raw().ExecuteServerCommandWithResponse(ctx, nil, generated.CommandRequest{Language: "sql", Command: "restore database koujou_graph_restored_test file:///home/arcadedb/backups/koujou_graph_test/graph-fixture.zip"})
	if err != nil || response.StatusCode() > 299 {
		t.Fatalf("restore: err=%v status=%d body=%s", err, response.StatusCode(), response.Body)
	}
	restored := remote.Server.DB("koujou_graph_restored_test")
	original := remote.DB
	remote.DB = restored
	restoredView := query("alice", Request{Root: document})
	if !bytes.Equal(encode(backupView), encode(restoredView)) {
		t.Fatal("backup changed IDs, versions, relationships, provenance or checkpoint")
	}
	if got := query("alice", Request{Root: latest, Kinds: []string{"version"}, Direction: "out"}); got.Meta.Completeness.State != "complete" || len(got.Data.Items) != 3 {
		t.Fatal("restored graph diverged", got)
	}
	remote.DB = original
	container := graphContainer(t)
	fixtureDocker(t, "stop", "--time", "2", container)
	request("alice", "POST", "/graph/workspaces/one/query", Request{Root: document}, 503)
	rejected(Request{Root: document}, "graph_unavailable")
	retained, err := c.Read(ctx, access.Principal{UserID: "alice"}, "one", manifestRef)
	if err != nil || !bytes.Equal(before.Value, retained.Value) {
		t.Fatal("graph outage changed Manifest")
	}
	fixtureDocker(t, "start", container)
	ready := false
	for start := time.Now(); time.Since(start) < 40*time.Second; time.Sleep(200 * time.Millisecond) {
		if _, err = remote.Active(ctx, "one"); err == nil {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("graph restart not ready")
	}
	if got := query("alice", Request{Root: latest, Kinds: []string{"version"}, Direction: "out"}); got.Meta.Completeness.State != "complete" || len(got.Data.Items) != 3 {
		t.Fatal("WAL restart diverged", got)
	}
	// Deleting retained changes simulates a restored SoT whose former cursor no
	// longer exists. The worker must rebuild, never skip to the current marker.
	prior, _ = remote.Active(ctx, "one")
	_, err = store.Database().Exec(ctx, "DELETE FROM shared_changes WHERE workspace_id='one'")
	if err != nil {
		t.Fatal(err)
	}
	extra := put("sentence", "history-gap", "0", contexts.Unit{Kind: "sentence", Text: &text})
	if err = s.Sync(ctx); err != nil {
		t.Fatal("history gap recovery", err)
	}
	recovered, _ := remote.Active(ctx, "one")
	if recovered.Active == prior.Active {
		t.Fatal("history gap not rebuilt")
	}
	if got := query("alice", Request{Root: extra, Kinds: []string{"version"}, Direction: "out"}); got.Meta.Completeness.State != "complete" || len(got.Data.Items) != 3 {
		t.Fatal("history gap lost source", got)
	}
	deleted, err := store.Apply(ctx, persistence.Scope{ActorID: "fixture", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: "context:" + extra.ID, Kind: "context-sentence", Deleted: true, Mutation: mutation(extra.Revision), Payload: encode(map[string]bool{"deleted": true})}, func(pgx.Tx, apicontract.Reference) (json.RawMessage, error) {
		return encode(map[string]bool{"deleted": true}), nil
	})
	if err != nil || deleted.Data == nil {
		t.Fatal("delete fixture", err)
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	removed := query("alice", Request{Root: contexts.Ref{Kind: "sentence", ID: extra.ID}, Kinds: []string{"version"}})
	if len(removed.Data.Items) != 1 || removed.Data.Items[0].Node == nil || !removed.Data.Items[0].Node.Deleted {
		t.Fatal("deleted identity retained current version")
	}
	t.Log("AC-3/4: actual graph outage denied new Manifest, prior Manifest unchanged, persistent WAL restart and SoT history-gap rebuild passed")
	t.Log("AC-2/4: multiple native snapshot commits, interrupted-stage isolation, concurrent content/membership/adopted relation catch-up, pre-commit rollback/retry and actual native backup/restore passed")
}
