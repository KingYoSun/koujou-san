package workbench

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
)

func TestWorkbenchPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:workbench")
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
	for _, id := range []string{"admin", "editor", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, id)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider');INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','editor','editor',false),('one','viewer','viewer',false),('two','outsider','admin',true);INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('device','one','admin','fixture','ビルド端末','g',true,true)`)
	key := []byte(strings.Repeat("w", 32))
	a := access.New(store, key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.Header.Get("X-Fixture-User")
		return id, id != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	rs := runs.New(store, a, c)
	fs := workflows.New(store, a, c, rs)
	s := New(store, a, rs, fs)
	admin, editor, viewer := access.Principal{UserID: "admin"}, access.Principal{UserID: "editor"}, access.Principal{UserID: "viewer"}
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("work-", serial), ExpectedVersion: version}
	}
	accept := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatalf("step %d: %+v %v", serial, v, err)
		}
		return Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("device\x00device\x00g"))
	device, err := a.DevicePrincipal(ctx, "one", "d.device."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	accept(rs.ContactRunner(ctx, device, "one", runs.RunnerContact{Mutation: mutation("0"), RunnerID: "runner", Connections: []runs.Connection{{Kind: "cli", Target: "fixture", Version: "v1", Environment: "fixture", Subject: "fixture", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"pause"}, Users: []string{"admin"}}}}))
	artifact := contexts.Fixed{URI: "https://example.test/build", Revision: "build-v1", Digest: strings.Repeat("a", 64), Range: "whole", Delivery: "reference", State: "unfetched"}
	spec := runs.Spec{Name: "固定ビルドの表示を確認する", Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture", Binding: map[string]string{"prompt": "stdin"}}, Prompt: raw("NEVER_COPY_INPUT_INTO_LIST")}, Artifacts: []contexts.Fixed{artifact}, Derivation: &runs.Contract{Name: "HumanReviewRun", Version: "one", VerdictField: "verdict"}}
	root := accept(rs.Create(ctx, admin, "one", "run:build", runs.Create{Mutation: mutation("0"), Spec: spec}))
	before, err := rs.Read(ctx, viewer, "one", root)
	if err != nil {
		t.Fatal(err)
	}
	var initial runs.Run
	_ = json.Unmarshal(before.Value, &initial)
	inputBefore, err := rs.Read(ctx, viewer, "one", initial.Input)
	if err != nil {
		t.Fatal(err)
	}
	root = accept(rs.Assign(ctx, admin, "one", root.ID, runs.Assign{Mutation: mutation(root.Version), DeviceID: "device"}))
	head := func() (runs.Record, runs.Run) {
		t.Helper()
		r, e := rs.Read(ctx, editor, "one", Ref{ID: root.ID, Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		var value runs.Run
		_ = json.Unmarshal(r.Value, &value)
		return r, value
	}
	_, value := head()
	if value.Assignment == nil {
		t.Fatal("fixture not assigned", value)
	}
	generation := value.Assignment.Generation
	sequence := 0
	report := func(event runs.Event) {
		t.Helper()
		sequence++
		event.Mutation = mutation(root.Version)
		event.Generation = generation
		event.Sequence = fmt.Sprint(sequence)
		root = accept(rs.Report(ctx, device, "one", root.ID, event))
	}
	report(runs.Event{Type: "prepared"})
	report(runs.Event{Type: "started"})
	report(runs.Event{Type: "pause", Pause: &runs.PauseRequest{ID: "review", Reason: "人による表示確認待ち", Request: raw(map[string]any{"question": "メニューの文字を読めますか？", "background": "固定ビルドの表示を確認します。", "options": []map[string]string{{"value": "PASS", "label": "読める", "impact": "次の工程へ進めます"}, {"value": "INCONCLUSIVE", "label": "判断できない", "impact": "理由を記録して確認を残します"}}, "waiting_for": "表示の検品", "instructions": "固定版のプレビューを開いて文字を確認してください。"})}})
	paused, pausedValue := head()
	if pausedValue.ActivePause == nil {
		t.Fatal("pause missing")
	}
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	route := Write{Mutation: mutation("0"), Note: Note{Kind: "route", Subject: paused.Ref, Pause: pausedValue.ActivePause, Recipient: "editor", Reason: "表示を確認できる担当へ依頼"}}
	routeRef := accept(s.Put(ctx, editor, "one", "work-note:route", route))
	if got := accept(s.Put(ctx, editor, "one", "work-note:route", route)); got != routeRef {
		t.Fatal("receipt changed")
	}
	for _, p := range []access.Principal{viewer, {UserID: "outsider"}} {
		if v, e := s.Put(ctx, p, "one", "work-note:denied", Write{Mutation: mutation("0"), Note: route.Note}); e == nil && v.Error == nil {
			t.Fatal("unauthorized note")
		}
	}
	bad := route.Note
	bad.Recipient = "viewer"
	if v, e := s.Put(ctx, editor, "one", "work-note:recipient", Write{Mutation: mutation("0"), Note: bad}); e == nil && v.Error == nil {
		t.Fatal("routing granted answer rights")
	}
	page, err := s.Query(ctx, editor, "one", Query{Kind: "work", Mine: true, Limit: 1})
	if err != nil || page.Data == nil || len(page.Data.Items) != 1 {
		t.Fatal("work query", page, err)
	}
	if bytes.Contains(raw(page), []byte("NEVER_COPY_INPUT")) || !bytes.Contains(raw(page), []byte("ビルド端末")) {
		t.Fatal("summary leaked input or lost device", string(raw(page)))
	}
	if !bytes.Contains(page.Data.Items[0].Value, []byte("表示の検品")) {
		t.Fatal("current stage missing", page)
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, routeRef.ID)
	privateRoute, err := s.Query(ctx, editor, "one", Query{Kind: "work", Mine: true})
	if err != nil || len(privateRoute.Data.Items) != 0 {
		t.Fatal("private routing leaked into summary", privateRoute, err)
	}
	exec(`UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id=$1`, routeRef.ID)
	notes, err := s.Query(ctx, viewer, "one", Query{Kind: "history", Subject: &paused.Ref, Limit: 1})
	if err != nil || len(notes.Data.Items) != 1 || notes.Data.Next != "" {
		t.Fatal("receipt duplicated history", notes, err)
	}
	// The second member may answer without gaining execution rights. Neither
	// receipt nor routing fabricates delivery or actual continuation.
	root = accept(rs.Answer(ctx, editor, "one", root.ID, runs.Answer{Mutation: mutation(root.Version), PauseID: "review", Value: raw(map[string]string{"verdict": "INCONCLUSIVE", "reason": "表示の倍率を確認できません", "evidence": "https://example.test/review-note"})}))
	_, value = head()
	if value.State != "paused" {
		t.Fatal("answer resumed execution")
	}
	pr, err := rs.Read(ctx, viewer, "one", *value.ActivePause)
	if err != nil {
		t.Fatal(err)
	}
	var pause runs.Pause
	_ = json.Unmarshal(pr.Value, &pause)
	if pause.State != "received" || pause.DeliveredAt != nil || pause.ResumedAt != nil || pause.AnsweredBy != "editor" {
		t.Fatal("receipt/delivery confused", pause)
	}
	report(runs.Event{Type: "delivered", PauseID: "review", ResponseID: pause.ResponseID})
	_, value = head()
	if value.State != "paused" {
		t.Fatal("delivery resumed execution")
	}
	report(runs.Event{Type: "resumed", PauseID: "review", ResponseID: pause.ResponseID})
	zero := 0
	report(runs.Event{Type: "finished", ExitCode: &zero, Report: raw(map[string]string{"verdict": "FAIL"})})
	failed, failedValue := head()
	if failedValue.Assessment.Verdict != "FAIL" {
		t.Fatal("verdict missing")
	}
	returned := Note{Kind: "return", Subject: failed.Ref, Reason: "表示を修正して再検品する", Targets: []contexts.Fixed{artifact}, Criteria: []string{"AC-build-text"}, Problem: "メニューの文字が切れる", Expected: "固定版の全ラベルを読める"}
	returnRef := accept(s.Put(ctx, editor, "one", "work-note:return", Write{Mutation: mutation("0"), Note: returned}))
	spec.Previous = &failed.Ref
	spec.Name = "修正Run（固定接続）"
	repair := accept(rs.Create(ctx, editor, "one", "run:repair", runs.Create{Mutation: mutation("0"), Spec: spec}))
	finish := func(ref Ref, verdict string) Ref {
		t.Helper()
		ref = accept(rs.Assign(ctx, admin, "one", ref.ID, runs.Assign{Mutation: mutation(ref.Version), DeviceID: "device"}))
		row, e := rs.Read(ctx, editor, "one", ref)
		if e != nil {
			t.Fatal(e)
		}
		var run runs.Run
		_ = json.Unmarshal(row.Value, &run)
		for i, event := range []runs.Event{{Type: "prepared"}, {Type: "started"}, {Type: "finished", ExitCode: &zero, Report: raw(map[string]string{"verdict": verdict})}} {
			event.Mutation = mutation(ref.Version)
			event.Generation = run.Assignment.Generation
			event.Sequence = fmt.Sprint(i + 1)
			ref = accept(rs.Report(ctx, device, "one", ref.ID, event))
		}
		return ref
	}
	repair = finish(repair, "PASS")
	spec.Previous = &repair
	spec.Name = "再検品Run（固定接続）"
	review := accept(rs.Create(ctx, editor, "one", "run:reinspection", runs.Create{Mutation: mutation("0"), Spec: spec}))
	review = finish(review, "PASS")
	saved, err := s.Read(ctx, viewer, "one", returnRef)
	if err != nil || saved.Value.Subject != failed.Ref {
		t.Fatal("return subject missing", saved, err)
	}
	for _, pair := range []struct{ source, next Ref }{{failed.Ref, repair}, {repair, review}} {
		links, err := rs.Query(ctx, viewer, "one", runs.Request{RunID: pair.source.ID, Version: pair.source.Version, Kind: "related", Limit: 50})
		if err != nil || len(links.Data.Items) != 1 || !bytes.Contains(links.Data.Items[0].Value, []byte(pair.next.ID)) {
			t.Fatal("existing Run link missing", links, err)
		}
	}
	after, err := rs.Read(ctx, viewer, "one", failed.Ref)
	if err != nil || !bytes.Equal(failed.Value, after.Value) {
		t.Fatal("old verdict changed")
	}
	inputAfter, err := rs.Read(ctx, viewer, "one", initial.Input)
	if err != nil || !bytes.Equal(inputBefore.Value, inputAfter.Value) {
		t.Fatal("view/notes changed input")
	}
	request, _ := http.NewRequest("GET", server.URL+"/workbench/workspaces/one/input/"+failed.ID+"?version="+failed.Version, nil)
	request.Header.Set("X-Fixture-User", "viewer")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || bytes.Contains(metadata, []byte("NEVER_COPY_INPUT")) || !bytes.Contains(metadata, []byte(artifact.URI)) {
		t.Fatal("input metadata boundary", response.StatusCode, string(metadata))
	}
	graph := workflows.Definition{Schema: "workflow-definition-v1", Name: "表示の受入工程", Budget: &workflows.Budget{MaxRuns: 1, Parallel: 1}, Nodes: []workflows.Node{{ID: "表示の判断", Choices: []string{"approve", "INCONCLUSIVE"}}}}
	definition := accept(fs.Put(ctx, editor, "one", "workflow-definition:review", workflows.Write{Mutation: mutation("0"), Value: raw(graph)}))
	verified := accept(fs.Verify(ctx, editor, "one", workflows.Verify{Mutation: mutation("0"), Definition: definition}))
	adopted := accept(fs.Adopt(ctx, editor, "one", workflows.Adopt{Mutation: mutation("0"), Definition: definition, Validation: verified}))
	execution := accept(fs.Start(ctx, admin, "one", "workflow-execution:review", workflows.Start{Mutation: mutation("0"), Adoption: adopted}))
	if err := fs.Step(ctx, "one", execution.ID); err != nil {
		t.Fatal(err)
	}
	executionRecord, err := fs.Read(ctx, editor, "one", Ref{ID: execution.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	accept(s.Put(ctx, editor, "one", "work-note:workflow-route", Write{Mutation: mutation("0"), Note: Note{Kind: "route", Subject: executionRecord.Ref, Recipient: "editor", Reason: "工程の判断を依頼"}}))
	workflowsPage, err := s.Query(ctx, editor, "one", Query{Kind: "work", Mine: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range workflowsPage.Data.Items {
		if item.ID == execution.ID && bytes.Contains(item.Value, []byte(graph.Name)) && bytes.Contains(item.Value, []byte("表示の判断")) {
			found = true
		}
	}
	if !found {
		t.Fatal("workflow missing from common work list", workflowsPage)
	}
	accept(fs.Decide(ctx, editor, "one", execution.ID, workflows.Decision{Mutation: mutation(executionRecord.Version), Node: "表示の判断", Attempt: 1, Choice: "INCONCLUSIVE"}))
	page, err = s.Query(ctx, viewer, "one", Query{Kind: "work", Limit: 1})
	if err != nil || page.Data.Next == "" {
		t.Fatal("work page missing", page, err)
	}
	second, err := s.Query(ctx, viewer, "one", Query{Kind: "work", Limit: 1, After: page.Data.Next})
	if err != nil || second.Data.Items[0].ID == page.Data.Items[0].ID {
		t.Fatal("page repeated")
	}
	if _, err = s.Query(ctx, access.Principal{UserID: "outsider"}, "one", Query{Kind: "work"}); err == nil {
		t.Fatal("foreign work visible")
	}
	// A current restriction applies to notes and list rows, not only the old
	// subject version. No read path grants access via a reference.
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, failed.ID)
	if _, err = s.Read(ctx, editor, "one", returnRef); err == nil {
		t.Fatal("note bypassed current restriction")
	}
	page, err = s.Query(ctx, editor, "one", Query{Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Data.Items {
		if item.ID == failed.ID {
			t.Fatal("restricted work in list")
		}
	}
	t.Log("paged summaries, same receipt, current routing rights, second-member INCONCLUSIVE answer, received/delivered/resumed, fixed return/repair/reinspection and unchanged input/verdict passed")
}
