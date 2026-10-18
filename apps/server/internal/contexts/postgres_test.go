package contexts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func TestContextsPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:contexts")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, u := range []string{"alice", "bob", "outsider"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO auth_users VALUES($1,'context-fixture',$1,$1||'@koujou.test',$1,false)", u); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("c", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		actor := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if actor != "alice" && actor != "bob" && actor != "outsider" {
			w.WriteHeader(401)
			return "", false
		}
		return actor, true
	})
	s := New(store, a, DefaultLimits())
	mux := http.NewServeMux()
	a.Register(mux)
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(actor, method, path string, body any, want int) []byte {
		t.Helper()
		var input io.Reader
		if body != nil {
			input = bytes.NewReader(canonical(body))
		}
		req, _ := http.NewRequest(method, server.URL+path, input)
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, response.StatusCode, want, raw)
		}
		return raw
	}
	n := 0
	mutation := func(version string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: fmt.Sprintf("op-%d", n), ExpectedVersion: version}
	}
	path := func(w, kind, id string) string { return "/contexts/workspaces/" + w + "/" + kind + "/" + id }
	for _, w := range []string{"one", "two"} {
		request("alice", "POST", "/access/workspaces/"+w+"/create", map[string]any{"id": w, "name": w, "operation_id": "create-" + w, "expected_version": "0"}, 200)
	}
	if _, err = store.Database().Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','bob','viewer')"); err != nil {
		t.Fatal(err)
	}
	put := func(w, kind, id, version string, value any) Ref {
		t.Helper()
		raw := request("alice", "POST", path(w, kind, id), Write{Mutation: mutation(version), Value: canonical(value)}, 200)
		var result apicontract.Response[apicontract.Reference]
		if err := json.Unmarshal(raw, &result); err != nil || result.Data == nil {
			t.Fatalf("write: %s", raw)
		}
		return Ref{kind, id, result.Data.Version}
	}
	read := func(actor, w string, r Ref) Record {
		t.Helper()
		raw := request(actor, "GET", path(w, r.Kind, r.ID)+"?revision="+r.Revision, nil, 200)
		var response apicontract.Response[Record]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil || response.Meta.Position == nil {
			t.Fatalf("read: %s", raw)
		}
		return *response.Data
	}
	preview := func(actor string, r ResolveRequest) Resolution {
		t.Helper()
		raw := request(actor, "POST", "/contexts/workspaces/one/resolve", r, 200)
		var response apicontract.Response[Resolution]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			t.Fatalf("preview: %s", raw)
		}
		return *response.Data
	}
	units, exampleRefs := fixtureGraph()
	saved := map[Ref]Ref{}
	for _, r := range exampleRefs {
		u := units[r]
		u.Children = append([]Ref{}, u.Children...)
		for i, c := range u.Children {
			u.Children[i] = saved[c]
		}
		actual := put("one", r.Kind, r.ID, "0", u)
		saved[r] = actual
		_ = read("bob", "one", actual)
	}
	s1, s2, p1, p2, section, doc := saved[exampleRefs[0]], saved[exampleRefs[1]], saved[exampleRefs[2]], saved[exampleRefs[3]], saved[exampleRefs[4]], saved[exampleRefs[5]]
	parentBefore := read("alice", "one", doc)
	request("bob", "POST", path("one", "sentence", "forbidden"), Write{mutation("0"), canonical(Unit{Kind: "sentence", Text: ptr("no")})}, 403)
	request("outsider", "GET", path("one", doc.Kind, doc.ID), nil, 403)
	request("bob", "GET", path("two", doc.Kind, doc.ID), nil, 403)
	request("d.fake.signature", "GET", path("one", doc.Kind, doc.ID), nil, 401)
	bad := []Unit{{Kind: "document", Children: []Ref{s1}}, {Kind: "paragraph", Children: []Ref{{"sentence", "missing", "r1"}}}, {Kind: "paragraph", Children: []Ref{{"paragraph", "cycle", "r1"}}}, {Kind: "paragraph", Children: []Ref{{"sentence", s1.ID, "head"}}}}
	var before int64
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM shared_changes").Scan(&before)
	for i, u := range bad {
		request("alice", "POST", path("one", u.Kind, fmt.Sprintf("bad-%d", i)), Write{mutation("0"), canonical(u)}, 400)
	}
	var after int64
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM shared_changes").Scan(&after)
	if before != after {
		t.Fatal("failed registration committed shared state")
	}
	t.Log("AC-1: all four kinds, typed ordered references, shared child, missing/type/cycle and fixed-revision rejection passed")

	fragment := Fixed{URI: "fixture://document", Revision: "source-v1", Range: "sentence:2", Digest: digest([]byte(`"固定した記述。"`)), Delivery: "snapshot", State: "ready", Structure: "sentence", Snapshot: []byte(`"固定した記述。"`)}
	external := put("one", "sentence", "external-fragment", "0", Unit{Kind: "sentence", External: &fragment, Origin: &Origin{Kind: "import", ID: "source-document", Revision: "source-v1", Digest: digest([]byte("whole original document")), Path: "sentence:2", OperationID: "import-op"}})
	var imported Unit
	_ = json.Unmarshal(read("bob", "one", external).Value, &imported)
	if imported.Origin.ID != "source-document" || imported.External.Range != "sentence:2" {
		t.Fatal("fixed source origin lost")
	}
	if resolved := preview("bob", ResolveRequest{Selection: Selection{Mode: "select", Include: []Ref{external}}}); !resolved.Ready || !strings.Contains(*resolved.Manifest.InputUTF8, "固定した記述。") {
		t.Fatal("fixed external sentence not reconstructed")
	}

	memberships := []Ref{}
	var documentMembership Ref
	for _, kind := range []string{"product", "project", "milestone", "issue"} {
		scope := put("one", "scope", kind, "0", Scope{Kind: kind, Name: kind})
		for _, r := range []Ref{s1, p1, section, doc} {
			m := put("one", "membership", kind+"-"+r.Kind, "0", Membership{Scope: Identity{kind, scope.ID}, Context: r.Identity(), Present: true})
			memberships = append(memberships, m)
			if kind == "issue" && r.Kind == "document" {
				documentMembership = m
			}
		}
	}
	for i := 0; i < 101; i++ {
		put("two", "scope", fmt.Sprintf("catalog-%03d", i), "0", Scope{Kind: "issue", Name: fmt.Sprintf("Scope %03d", i)})
	}
	catalog := func(actor, workspace string, requestBody CatalogRequest, status int) apicontract.Response[CatalogPage] {
		t.Helper()
		raw := request(actor, "POST", "/contexts/workspaces/"+workspace+"/catalog", requestBody, status)
		var value apicontract.Response[CatalogPage]
		_ = json.Unmarshal(raw, &value)
		return value
	}
	firstCatalog := catalog("alice", "two", CatalogRequest{Kind: "scope"}, 200)
	if firstCatalog.Data == nil || len(firstCatalog.Data.Items) != 100 || firstCatalog.Meta.NextCursor == nil {
		t.Fatalf("Scope catalog did not return bounded metadata: %+v %+v", firstCatalog.Data, firstCatalog.Meta)
	}
	lastCatalog := catalog("alice", "two", CatalogRequest{Kind: "scope", After: *firstCatalog.Meta.NextCursor}, 200)
	if lastCatalog.Data == nil || len(lastCatalog.Data.Items) != 1 || lastCatalog.Meta.NextCursor != nil {
		t.Fatal("Scope catalog continuation repeated or lost Scope")
	}
	catalog("bob", "two", CatalogRequest{Kind: "scope"}, 403)
	memberHistory := catalog("bob", "one", CatalogRequest{Kind: "membership", ID: documentMembership.ID}, 200)
	if memberHistory.Data == nil || len(memberHistory.Data.Items) != 1 || memberHistory.Data.Items[0].Ref != documentMembership {
		t.Fatal("per-identity history did not preserve fixed version")
	}

	var dm Membership
	_ = json.Unmarshal(read("alice", "one", documentMembership).Value, &dm)
	dm.Present = false
	removed := put("one", "membership", documentMembership.ID, documentMembership.Revision, dm)
	var original Membership
	_ = json.Unmarshal(read("alice", "one", documentMembership).Value, &original)
	if !original.Present {
		t.Fatal("membership history overwritten")
	}
	for _, m := range memberships {
		if m == documentMembership {
			continue
		}
		var current Membership
		_ = json.Unmarshal(read("bob", "one", Ref{m.Kind, m.ID, "head"}).Value, &current)
		if !current.Present {
			t.Fatal("parent grouping changed child/other grouping")
		}
	}
	if !bytes.Equal(read("alice", "one", doc).Value, parentBefore.Value) {
		t.Fatal("membership edited content")
	}
	t.Log("AC-2: independent membership history for every grain and Scope; parent changes do not propagate")

	selection := Selection{Mode: "select", Include: []Ref{doc, s1, doc}}
	baseline := Baseline{Contexts: []Ref{s1, s2, p1, p2, section, doc}, Memberships: memberships, Inputs: []PlannedInput{{"run-a", selection}, {"run-b", Selection{Mode: "select", Include: []Ref{s2}}}}}
	base := put("one", "baseline", "workflow", "0", baseline)
	initialRequest := ResolveRequest{RunID: "run-a", Baseline: &base, Selection: selection}
	mref := put("one", "manifest", "started-input", "0", initialRequest)
	initialRecord := read("bob", "one", mref)
	var initial Manifest
	_ = json.Unmarshal(initialRecord.Value, &initial)
	if initial.InputUTF8 == nil || len(initial.Placements) != 7 || len(initial.Duplicates) != 2 || initial.ReadStatus != "unobserved" {
		t.Fatal("initial structure/metadata not fixed")
	}
	changedText := "明示的に更新した文。"
	updateBody := Write{mutation(s1.Revision), canonical(Unit{Kind: "sentence", Text: &changedText})}
	rawUpdate := request("alice", "POST", path("one", "sentence", s1.ID), updateBody, 200)
	if !bytes.Equal(rawUpdate, request("alice", "POST", path("one", "sentence", s1.ID), updateBody, 200)) {
		t.Fatal("mutation replay changed result")
	}
	var updated apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(rawUpdate, &updated)
	newS1 := Ref{s1.Kind, s1.ID, updated.Data.Version}
	request("alice", "POST", path("one", "sentence", s1.ID), Write{mutation(s1.Revision), canonical(Unit{Kind: "sentence", Text: ptr("stale")})}, 409)
	changedOperation := updateBody
	changedOperation.Value = canonical(Unit{Kind: "sentence", Text: ptr("different")})
	request("alice", "POST", path("one", "sentence", s1.ID), changedOperation, 409)
	if !bytes.Equal(read("alice", "one", doc).Value, parentBefore.Value) {
		t.Fatal("child update changed parent revision")
	}
	if got := preview("alice", initialRequest); !got.Ready || got.Manifest.Digest != initial.Digest {
		t.Fatal("shared update changed baseline input")
	}
	newP1 := put("one", "paragraph", p1.ID, p1.Revision, Unit{Kind: "paragraph", Children: []Ref{newS1, s2}})
	newP2 := put("one", "paragraph", p2.ID, p2.Revision, Unit{Kind: "paragraph", Children: []Ref{newS1}})
	newSection := put("one", "section", section.ID, section.Revision, Unit{Kind: "section", Title: "判断の基準", Children: []Ref{newP1, newP2}})
	newDoc := put("one", "document", doc.ID, doc.Revision, Unit{Kind: "document", Title: "製品方針", Children: []Ref{newSection}})
	t.Log("AC-3: child revisions and explicit parent adoption preserve every historical parent")

	for _, mode := range []string{"none", ""} {
		r := preview("bob", ResolveRequest{Baseline: &base, Selection: Selection{Mode: mode}})
		if !r.Ready || r.Manifest.InputUTF8 != nil || r.Manifest.State != "none" {
			t.Fatal("implicit context inheritance")
		}
	}
	conflict := preview("alice", ResolveRequest{Selection: Selection{Mode: "none", Direct: []Direct{{Value: []byte(`"direct"`)}}}})
	if conflict.Ready || conflict.Missing[0].Code != "selection_conflict" {
		t.Fatal("none bypassed by direct value")
	}
	excerpt := selection
	excerpt.Exclude = []Ref{s1}
	result := preview("bob", ResolveRequest{Selection: excerpt})
	if !result.Ready || len(result.Manifest.Placements) != 4 || strings.Contains(*result.Manifest.InputUTF8, "共有の原文。") {
		t.Fatal("exclusion did not apply to all paths")
	}
	collision := preview("alice", ResolveRequest{Selection: Selection{Mode: "select", Include: []Ref{doc, newS1}}})
	if collision.Ready || collision.Missing[0].Code != "conflicting_revisions" {
		t.Fatal("automatic latest selection")
	}
	t.Log("AC-4/5: none/unspecified, explicit direct conflict, ordering, placements, duplicates, exclusion, excerpt and revision collision passed")

	baseline.Contexts = []Ref{newS1, s2, newP1, newP2, newSection, newDoc}
	newSelection := Selection{Mode: "select", Include: []Ref{newDoc, newS1, newDoc}}
	baseline.Inputs[0].Selection = newSelection
	nextBase := put("one", "baseline", base.ID, base.Revision, baseline)
	var adoption Baseline
	_ = json.Unmarshal(read("alice", "one", nextBase).Value, &adoption)
	if adoption.Previous == nil || *adoption.Previous != base || !reflect.DeepEqual(adoption.AffectedRunIDs, []string{"run-a"}) {
		t.Fatalf("adoption impact: %+v", adoption)
	}
	nextManifest := put("one", "manifest", "next-input", "0", ResolveRequest{RunID: "next-run", Baseline: &nextBase, Selection: newSelection})
	var next Manifest
	_ = json.Unmarshal(read("alice", "one", nextManifest).Value, &next)
	if next.Digest == initial.Digest {
		t.Fatal("explicit baseline adoption not reflected")
	}
	if !bytes.Equal(read("alice", "one", mref).Value, initialRecord.Value) {
		t.Fatal("started input mutated")
	}
	request("alice", "POST", path("one", "manifest", mref.ID), Write{mutation(mref.Revision), canonical(initialRequest)}, 400)
	baseline.Memberships = []Ref{removed}
	thirdBase := put("one", "baseline", base.ID, nextBase.Revision, baseline)
	var membershipAdoption Baseline
	_ = json.Unmarshal(read("alice", "one", thirdBase).Value, &membershipAdoption)
	if len(membershipAdoption.AffectedRunIDs) != 2 {
		t.Fatal("membership basis change not recorded")
	}
	t.Log("AC-6: immutable Workflow basis, explicit adoption/impact and fixed started Manifest passed")

	fixed := Fixed{URI: "fixture://external", Revision: "r1", Range: "whole", Digest: digest([]byte(`"source"`)), Delivery: "snapshot", State: "unfetched", Structure: "unstructured"}
	missingExternal := preview("alice", ResolveRequest{Selection: Selection{Mode: "select", External: []Fixed{fixed}}})
	if missingExternal.Ready || missingExternal.Missing[0].Code != "external_unfetched" {
		t.Fatal("unfetched external accepted")
	}
	partial := preview("alice", ResolveRequest{Selection: selection, Evidence: []CandidateEvidence{{Engine: "bleve", Position: "p1", RequiredPosition: "p2", State: "pending", Reason: "projection_lag"}}})
	if partial.Ready || partial.Manifest != nil {
		t.Fatal("incomplete candidates became complete")
	}
	s.Limits = Limits{64, 100000, 100000}
	limitFailure := request("alice", "POST", path("one", "manifest", "too-large"), Write{mutation("0"), canonical(initialRequest)}, 400)
	if !bytes.Contains(limitFailure, []byte("byte_limit")) {
		t.Fatal("capacity reason missing")
	}
	s.Limits = DefaultLimits()
	var count int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM context_records WHERE resource_id='manifest:too-large'").Scan(&count)
	if count != 0 {
		t.Fatal("incomplete manifest persisted")
	}

	output := Direct{Value: []byte(`{"answer":"原出力"}`), Origin: &Origin{Kind: "output", ID: "source-run", Revision: "output-v1", Digest: digest([]byte("whole original output before extraction")), Path: "/answer"}, State: "ready"}
	directManifest := put("one", "manifest", "output-input", "0", ResolveRequest{RunID: "output-consumer", Selection: Selection{Mode: "select", Direct: []Direct{output}}})
	addition := put("one", "addition", "pause-response", "0", Addition{Manifest: directManifest, PauseID: "pause-a", ResponseID: "response-a", Context: output, ReadStatus: "read"})
	var added Addition
	_ = json.Unmarshal(read("bob", "one", addition).Value, &added)
	if added.ReadStatus != "unobserved" || added.Context.Origin.Revision != "output-v1" || !bytes.Equal(added.Context.Value, canonical(output.Value)) {
		t.Fatal("additional input provenance/read status lost")
	}
	var originalOutput Manifest
	_ = json.Unmarshal(read("bob", "one", directManifest).Value, &originalOutput)
	if originalOutput.Selection.Direct[0].Origin.Digest != output.Origin.Digest {
		t.Fatal("output digest lost")
	}
	if !bytes.Equal(read("alice", "one", mref).Value, initialRecord.Value) {
		t.Fatal("pause addition changed input")
	}
	t.Log("AC-7/8: source/limit/projection failures do not finalize; direct output and pause origin remain separate from input")

	private := put("one", "sentence", "private", "0", Unit{Kind: "sentence", Text: ptr("private-context-body")})
	parent := put("one", "paragraph", "public-parent", "0", Unit{Kind: "paragraph", Children: []Ref{private}})
	privateManifest := put("one", "manifest", "private-input", "0", ResolveRequest{RunID: "private-run", Selection: Selection{Mode: "select", Include: []Ref{parent}}})
	policy := func(update bool) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: resource(private.Identity()), Restricted: true}, []access.ResourceGrant{{UserID: "alice", Read: true, Update: update}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	policy(true)
	_ = read("bob", "one", parent)
	request("bob", "GET", path("one", private.Kind, private.ID), nil, 403)
	denied := preview("bob", ResolveRequest{Selection: Selection{Mode: "select", Include: []Ref{parent}}})
	if denied.Ready || denied.Missing[0].Code != "forbidden" {
		t.Fatal("parent granted child access")
	}
	request("bob", "GET", path("one", "manifest", privateManifest.ID), nil, 403)
	put("one", "sentence", private.ID, private.Revision, Unit{Kind: "sentence", Text: ptr("private-edit")})
	request("bob", "GET", path("one", private.Kind, private.ID), nil, 403) // Editing must preserve restricted policy.
	policy(false)
	request("alice", "POST", path("one", private.Kind, private.ID), Write{mutation("0"), canonical(Unit{Kind: "sentence", Text: ptr("bypass")})}, 403)
	_, _ = store.Database().Exec(ctx, "UPDATE access_members SET active=false WHERE workspace_id='one' AND user_id='bob'")
	request("bob", "GET", path("one", doc.Kind, doc.ID), nil, 403)
	t.Log("INVAR-1..5: actual Context read/update/resolve/Manifest paths preserve Workspace and resource grants, including current revocation")
}
func ptr(s string) *string { return &s }
