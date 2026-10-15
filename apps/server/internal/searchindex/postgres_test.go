package searchindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/blevesearch/bleve/v2"
	"github.com/jackc/pgx/v5"
)

type indexDelegate = bleve.Index
type interruptedIndex struct {
	indexDelegate
	writes, stopAfter int
}

func (i *interruptedIndex) Batch(batch *bleve.Batch) error {
	i.writes++
	if i.writes > i.stopAfter {
		return errors.New("injected index stop")
	}
	return i.indexDelegate.Batch(batch)
}

func TestSearchPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:search")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alice", "bob", "outsider"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO auth_users VALUES($1,'search-fixture',$1,$1||'@koujou.test',$1,false)", id); err != nil {
			t.Fatal(err)
		}
	}
	key := []byte(strings.Repeat("k", 32))
	a := access.New(store, key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if id != "alice" && id != "bob" && id != "outsider" {
			w.WriteHeader(401)
			return "", false
		}
		return id, true
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	dir := t.TempDir()
	s := New(store, a, c, dir, key)
	defer func() { _ = s.Close() }()
	mux := http.NewServeMux()
	a.Register(mux)
	c.Register(mux)
	// Forward to the current component to exercise a real close/open restart.
	mux.Handle("/search/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := http.NewServeMux()
		s.Register(m)
		m.ServeHTTP(w, r)
	}))
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(actor, path string, body any, status int) []byte {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(encode(body)))
		req.Header.Set("Authorization", "Bearer "+actor)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("%s: got%d want%d %s", path, response.StatusCode, status, raw)
		}
		return raw
	}
	n := 0
	mutation := func(version string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: fmt.Sprintf("op-%d", n), ExpectedVersion: version}
	}
	for _, w := range []string{"one", "two"} {
		request("alice", "/access/workspaces/"+w+"/create", map[string]any{"id": w, "name": w, "operation_id": "create-" + w, "expected_version": "0"}, 200)
	}
	if _, err = store.Database().Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','bob','viewer')"); err != nil {
		t.Fatal(err)
	}
	put := func(w, kind, id, version string, value any) contexts.Ref {
		t.Helper()
		raw := request("alice", "/contexts/workspaces/"+w+"/"+kind+"/"+id, contexts.Write{Mutation: mutation(version), Value: encode(value)}, 200)
		var response apicontract.Response[apicontract.Reference]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			t.Fatalf("write: %s", raw)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: response.Data.Version}
	}
	search := func(actor, w string, r Request) apicontract.Response[Result] {
		t.Helper()
		raw := request(actor, "/search/workspaces/"+w+"/query", r, 200)
		var response apicontract.Response[Result]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			t.Fatalf("search: %s", raw)
		}
		return response
	}
	cases := qualityCases()
	saved := map[string][]contexts.Ref{}
	for number, example := range cases {
		for version := 0; version < 3; version++ {
			var child contexts.Ref
			for _, kind := range []string{"sentence", "paragraph", "section", "document"} {
				id := fmt.Sprintf("q%02d-%s", number, kind)
				u := contexts.Unit{Kind: kind, Name: id, Tags: []string{"quality", "selected"}}
				if kind == "sentence" {
					text := example.text
					u.Text = &text
				} else {
					u.Children = []contexts.Ref{child}
				}
				prior := "0"
				if versions := saved[id]; len(versions) > 0 {
					prior = versions[len(versions)-1].Revision
				}
				child = put("one", kind, id, prior, u)
				saved[id] = append(saved[id], child)
			}
		}
	}
	scope := contexts.Identity{Kind: "issue", ID: "quality-scope"}
	put("one", "scope", scope.ID, "0", contexts.Scope{Kind: scope.Kind, Name: "direct membership"})
	put("one", "membership", "doc-membership", "0", contexts.Membership{Scope: scope, Context: contexts.Identity{Kind: "document", ID: "q00-document"}, Present: true})
	private := saved["q02-sentence"][2]
	if err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: "context:" + private.ID, Restricted: true}, []access.ResourceGrant{{UserID: "alice", Read: true, Update: true}})
	}); err != nil {
		t.Fatal(err)
	}
	text := "other Workspace contains orchestration"
	put("two", "sentence", "private-other", "0", contexts.Unit{Kind: "sentence", Text: &text})
	if err = s.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	for _, example := range cases {
		result := search("alice", "one", example.request)
		found := false
		for _, item := range result.Data.Items {
			if item.Ref.ID == example.id && item.Ref.Revision == saved[example.id][2].Revision {
				found = true
			}
		}
		if !found || result.Meta.Completeness.State != "complete" {
			t.Fatalf("quality %s: %+v", example.word, result)
		}
	}
	direct := search("alice", "one", Request{Scopes: []contexts.Identity{scope}, Kinds: []string{"document"}, Limit: 10})
	inherited := search("alice", "one", Request{Scopes: []contexts.Identity{scope}, Kinds: []string{"sentence"}, Limit: 10})
	if direct.Data.Count != 1 || inherited.Data.Count != 0 {
		t.Fatal("Scope membership propagated into referenced sentences")
	}
	historical := search("alice", "one", Request{IDs: []string{"q00-sentence"}, Revisions: []string{saved["q00-sentence"][0].Revision}, Tags: []string{"quality"}, Limit: 10})
	if historical.Data.Count != 1 || historical.Data.Items[0].Ref != saved["q00-sentence"][0] {
		t.Fatal("fixed revision or tag filter failed")
	}
	if search("alice", "one", Request{Tags: []string{"absent"}, Limit: 10}).Data.Count != 0 {
		t.Fatal("tag filter ignored")
	}
	t.Log("AC-1: 40 Japanese/English/code/exact-ID answers in top10, four grains, fixed revisions, tags and direct Scope isolation passed")
	denied := search("bob", "one", cases[2].request)
	if denied.Data.Count != 0 || denied.Meta.NextCursor != nil {
		t.Fatal("private child disclosed a parent, count or continuation")
	}
	request("bob", "/search/workspaces/two/query", Request{Limit: 10}, 403)
	request("outsider", "/search/workspaces/one/query", Request{Limit: 10}, 403)
	page := search("bob", "one", Request{Kinds: []string{"sentence"}, Limit: 3})
	if page.Meta.NextCursor == nil {
		t.Fatal("no continuation")
	}
	request("alice", "/search/workspaces/one/query", Request{Kinds: []string{"sentence"}, Limit: 3, Cursor: *page.Meta.NextCursor}, 400)
	ids := map[string]bool{}
	for {
		for _, item := range page.Data.Items {
			if ids[item.Ref.ID] {
				t.Fatal("duplicate page result")
			}
			ids[item.Ref.ID] = true
		}
		if page.Meta.NextCursor == nil {
			break
		}
		page = search("bob", "one", Request{Kinds: []string{"sentence"}, Limit: 3, Cursor: *page.Meta.NextCursor})
	}
	if len(ids) != 39 || ids[private.ID] {
		t.Fatalf("authorized pages: %d", len(ids))
	}
	t.Log("AC-2: current child authorization, Workspace boundary, private counts and cursor actor binding passed")

	old := saved["q03-sentence"][2]
	changed := "新しい内容に置換した本文"
	next := put("one", "sentence", old.ID, old.Revision, contexts.Unit{Kind: "sentence", Text: &changed})
	pending := search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10})
	if pending.Meta.Completeness.State != "pending" {
		t.Fatal("delayed index claimed complete")
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	updated := search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10})
	if updated.Data.Count != 1 || updated.Data.Items[0].Ref != next {
		t.Fatal("new version not projected")
	}
	count, _ := s.index.DocCount()
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := s.index.DocCount()
	if count != again {
		t.Fatal("repeated delivery duplicated index")
	}
	generationBefore := s.generation
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = New(store, a, c, dir, key)
	reopened := search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10})
	if reopened.Data.Items[0].Ref != next || s.generation != generationBefore {
		t.Fatal("restart lost checkpoint/index")
	}
	t.Log("AC-3: update, repeated delivery and actual durable close/open preserved versions/checkpoint")

	// The active marker cannot be replaced. The candidate must be discarded while
	// the verified prior generation remains available; no synthetic fail hook.
	if err = os.Mkdir(filepath.Join(dir, "active.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = s.Rebuild(ctx); err == nil {
		t.Fatal("injected switch failure accepted")
	}
	if s.generation != generationBefore || search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10}).Data.Count != 1 {
		t.Fatal("failed rebuild replaced active index")
	}
	if err = os.Remove(filepath.Join(dir, "active.json.tmp")); err != nil {
		t.Fatal(err)
	}
	var membershipVersion string
	if err = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='one' AND resource_id='membership:doc-membership'").Scan(&membershipVersion); err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- s.Rebuild(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	observed := false
	for time.Now().Before(deadline) {
		var active bool
		err = store.Database().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_xmin IS NOT NULL AND query LIKE '%context_records%')`).Scan(&active)
		if err != nil {
			t.Fatal(err)
		}
		if active {
			observed = true
			break
		}
		select {
		case err = <-started:
			t.Fatalf("rebuild ended before concurrent update: %v", err)
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !observed {
		t.Fatal("snapshot transaction not observed")
	}
	newestText := "再構築と並行して更新した本文"
	newest := put("one", "sentence", old.ID, next.Revision, contexts.Unit{Kind: "sentence", Text: &newestText})
	put("one", "membership", "doc-membership", membershipVersion, contexts.Membership{Scope: scope, Context: contexts.Identity{Kind: "document", ID: "q00-document"}, Present: false})
	if err = <-started; err != nil {
		t.Fatal(err)
	}
	if result := search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10}); result.Data.Count != 1 || result.Data.Items[0].Ref != newest {
		t.Fatal("rebuild lost concurrent update")
	}
	if result := search("alice", "one", Request{Scopes: []contexts.Identity{scope}, Limit: 10}); result.Data.Count != 0 || result.Meta.Completeness.State != "complete" {
		t.Fatal("rebuild lost concurrent membership removal")
	}
	t.Log("AC-4: real marker-write failure retained old generation; parallel content and membership updates caught up before verified switch")
	var oldEvent persistence.Change
	var oldSequence int64
	err = store.Database().QueryRow(ctx, `SELECT sequence,position,resource_id,version,kind,deleted,payload FROM shared_changes WHERE workspace_id='one' AND resource_id=$1 ORDER BY sequence LIMIT 1`, "context:"+old.ID).Scan(&oldSequence, &oldEvent.Position, &oldEvent.ID, &oldEvent.Version, &oldEvent.Kind, &oldEvent.Deleted, &oldEvent.Payload)
	if err != nil {
		t.Fatal(err)
	}
	b := builder{store.Database(), map[string]contexts.Unit{}}
	if err = s.apply(ctx, &b, s.index, "one", oldSequence, oldEvent); err != nil {
		t.Fatal(err)
	}
	if got := search("alice", "one", Request{IDs: []string{old.ID}, Limit: 10}); got.Data.Items[0].Ref != newest {
		t.Fatal("late old event overwrote new revision")
	}
	// Stop after one of two queued updates is durable, then open the actual files
	// again. The incomplete range must remain pending until the second is replayed.
	beforeCP, err := readCheckpoint(s.index, "one")
	if err != nil {
		t.Fatal(err)
	}
	beforeCount, err := s.index.DocCount()
	if err != nil {
		t.Fatal(err)
	}
	firstText, secondText := "途中停止の前に保存した版", "途中停止から再開して保存した版"
	first := put("one", "sentence", "q04-sentence", saved["q04-sentence"][2].Revision, contexts.Unit{Kind: "sentence", Text: &firstText})
	second := put("one", "sentence", "q05-sentence", saved["q05-sentence"][2].Revision, contexts.Unit{Kind: "sentence", Text: &secondText})
	s.index = &interruptedIndex{indexDelegate: s.index, stopAfter: 1}
	if err = s.Sync(ctx); err == nil {
		t.Fatal("index interruption not injected")
	}
	middleCP, err := readCheckpoint(s.index, "one")
	if err != nil || middleCP.Sequence != beforeCP.Sequence+1 {
		t.Fatal("partial progress checkpoint not durable")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = New(store, a, c, dir, key)
	if result := search("alice", "one", Request{IDs: []string{first.ID, second.ID}, Limit: 10}); result.Meta.Completeness.State != "pending" {
		t.Fatal("interrupted range claimed complete after restart")
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	resumed := search("alice", "one", Request{IDs: []string{first.ID, second.ID}, Limit: 10})
	refs := map[contexts.Ref]bool{}
	for _, item := range resumed.Data.Items {
		refs[item.Ref] = true
	}
	afterCount, err := s.index.DocCount()
	if err != nil || afterCount != beforeCount+2 || !refs[first] || !refs[second] || resumed.Meta.Completeness.State != "complete" {
		t.Fatal("interrupted update did not reconcile without duplication")
	}
	t.Log("AC-3: stop after first durable batch/checkpoint, actual close/open pending state, remaining replay and exact revision/count convergence passed")
	selection := contexts.ResolveRequest{RunID: "search-run", Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{newest}}}
	searchSelection := ResolveRequest{Search: Request{IDs: []string{newest.ID}, Limit: 10}, Resolve: selection, ID: "search-manifest", Mutation: func() *apicontract.Mutation { m := mutation("0"); return &m }()}
	raw := request("alice", "/search/workspaces/one/resolve", searchSelection, 200)
	var accepted apicontract.Response[apicontract.Reference]
	if json.Unmarshal(raw, &accepted) != nil || accepted.Data == nil {
		t.Fatalf("search Manifest not accepted: %s", raw)
	}
	manifestRef := contexts.Ref{Kind: "manifest", ID: "search-manifest", Revision: accepted.Data.Version}
	beforeManifest, err := c.Read(ctx, access.Principal{UserID: "alice"}, "one", manifestRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	manifestCount := func() int {
		t.Helper()
		var count int
		if err := store.Database().QueryRow(ctx, "SELECT count(*) FROM context_records WHERE kind='manifest'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	rejectedSelection := func(searchRequest Request, ref contexts.Ref, reason string) {
		t.Helper()
		r := ResolveRequest{Search: searchRequest, Resolve: contexts.ResolveRequest{RunID: "blocked-run", Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{ref}}}, ID: "must-not-save-" + reason, Mutation: func() *apicontract.Mutation { m := mutation("0"); return &m }()}
		raw := request("alice", "/search/workspaces/one/resolve", r, 200)
		var response apicontract.Response[contexts.Resolution]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil || response.Data.Ready || len(response.Data.Missing) == 0 || response.Data.Missing[0].Code != "incomplete_candidates" || !strings.Contains(response.Data.Missing[0].Source, reason) {
			t.Fatalf("bad incomplete resolution %s", raw)
		}
		if manifestCount() != 1 {
			t.Fatal("incomplete search finalized Manifest")
		}
	}
	rejectedSelection(Request{Kinds: []string{"sentence"}, Limit: 1}, newest, "limit_reached")
	freshText := "索引反映を待つ新しい版"
	fresh := put("one", "sentence", newest.ID, newest.Revision, contexts.Unit{Kind: "sentence", Text: &freshText})
	rejectedSelection(Request{IDs: []string{fresh.ID}, Revisions: []string{fresh.Revision}, Limit: 10}, fresh, "projection_pending")
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.index.Delete(docID("one", fresh)); err != nil {
		t.Fatal(err)
	}
	rejectedSelection(Request{IDs: []string{fresh.ID}, Revisions: []string{fresh.Revision}, Limit: 10}, fresh, "required_version_pending")
	if err = s.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	request("alice", "/search/workspaces/one/query", Request{Limit: 10}, 503)
	rejectedSelection(Request{IDs: []string{fresh.ID}, Limit: 10}, fresh, "search_unavailable")
	s = New(store, a, c, dir, key)
	afterManifest, err := c.Read(ctx, access.Principal{UserID: "alice"}, "one", manifestRef)
	if err != nil || !bytes.Equal(beforeManifest.Value, afterManifest.Value) {
		t.Fatal("later search/index state changed fixed Manifest")
	}
	deleted, err := store.Apply(ctx, persistence.Scope{ActorID: "user:alice", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: "context:" + fresh.ID, Kind: "context-sentence", Deleted: true, Mutation: mutation(fresh.Revision), Payload: encode(map[string]bool{"deleted": true})}, func(pgx.Tx, apicontract.Reference) (json.RawMessage, error) {
		return encode(map[string]bool{"deleted": true}), nil
	})
	if err != nil || deleted.Error != nil {
		t.Fatalf("delete: %v %+v", err, deleted)
	}
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if result := search("alice", "one", Request{IDs: []string{fresh.ID}, Limit: 10}); result.Data.Count != 0 || result.Meta.Completeness.State != "complete" {
		t.Fatal("deleted context remained in search")
	}
	t.Log("AC-3/5: old event rejection, membership removal, deletion, actual search-to-Manifest acceptance, partial/pending/stopped/version-missing rejection and immutable prior Manifest passed")
	// Losing the saved update position triggers the production snapshot recovery.
	if _, err = store.Database().Exec(ctx, "DELETE FROM shared_changes WHERE workspace_id='one'"); err != nil {
		t.Fatal(err)
	}
	extraText := "履歴不足から再構築する本文"
	extra := put("one", "sentence", "after-history-loss", "0", contexts.Unit{Kind: "sentence", Text: &extraText})
	priorGeneration := s.generation
	if err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if s.generation == priorGeneration || search("alice", "one", Request{IDs: []string{extra.ID}, Limit: 10}).Data.Count != 1 {
		t.Fatal("lost history not rebuilt from SoT")
	}
	if err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: "context:" + extra.ID, Restricted: true}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if got := search("bob", "one", Request{IDs: []string{extra.ID}, Limit: 10}); got.Data.Count != 0 || got.Meta.NextCursor != nil {
		t.Fatal("policy-only revocation leaked result/count")
	}
	if _, err = store.Database().Exec(ctx, "UPDATE access_members SET active=false WHERE workspace_id='one' AND user_id='bob'"); err != nil {
		t.Fatal(err)
	}
	request("bob", "/search/workspaces/one/query", Request{Limit: 10}, 403)
	t.Log("AC-2/4: history-loss rebuild and permission-only revocation passed; INVAR-1..4 preserved")
}
