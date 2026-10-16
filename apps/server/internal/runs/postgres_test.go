package runs

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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

func TestRunsPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:runs")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := store.Database().Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, u := range []string{"admin", "editor", "second", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, u)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider');
 INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','editor','editor',false),('one','second','editor',false),('one','viewer','viewer',false),('two','outsider','admin',true);
 INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('device','one','admin','fixture','fixture','fixture-generation',true,true)`)
	key := []byte(strings.Repeat("k", 32))
	a := access.New(store, key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.Header.Get("X-Fixture-User")
		if id == "" {
			w.WriteHeader(401)
		}
		return id, id != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	s := New(store, a, c)
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("device\x00device\x00fixture-generation"))
	deviceToken := "d.device." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	var serial atomic.Int64
	mutation := func(version string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: fmt.Sprint("run-op-", serial.Add(1)), ExpectedVersion: version}
	}
	request := func(actor, method, path string, v any) (int, []byte) {
		t.Helper()
		var body io.Reader
		if v != nil {
			body = bytes.NewReader(raw(v))
		}
		req, e := http.NewRequest(method, server.URL+path, body)
		if e != nil {
			t.Fatal(e)
		}
		if actor == "device" {
			req.Header.Set("Authorization", "Bearer "+deviceToken)
		} else {
			req.Header.Set("X-Fixture-User", actor)
		}
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	path := func(id, action string) string { return "/runs/workspaces/one/executions/" + id + "/" + action }
	accept := func(actor, id, action string, v any) Ref {
		t.Helper()
		status, b := request(actor, "POST", path(id, action), v)
		var out apicontract.Response[apicontract.Reference]
		_ = json.Unmarshal(b, &out)
		if status != 200 || out.Data == nil {
			t.Fatalf("%s: status=%d body=%s", action, status, b)
		}
		return Ref{ID: out.Data.ID, Version: out.Data.Version}
	}
	command := func(kind, target string) Spec {
		return Spec{Input: Input{Command: Command{Kind: kind, Target: target, Parameters: raw([]string{"fixture"})}}}
	}
	create := func(id string, spec Spec) Ref {
		return accept("admin", id, "create", Create{Mutation: mutation("0"), Spec: spec})
	}
	read := func(ref Ref) Record {
		t.Helper()
		r, e := s.Read(ctx, access.Principal{UserID: "admin"}, "one", ref)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	head := func(ref Ref) Run {
		t.Helper()
		var r Run
		if json.Unmarshal(read(ref).Value, &r) != nil {
			t.Fatal("invalid run head")
		}
		return r
	}
	prepare := func(ref Ref) Ref {
		t.Helper()
		ref = accept("admin", ref.ID, "request-start", mutation(ref.Version))
		exec(`UPDATE access_resources SET assigned_device='device' WHERE workspace_id='one' AND id=$1`, ref.ID)
		return ref
	}
	report := func(ref Ref, e Event) Ref {
		t.Helper()
		e.Mutation = mutation(ref.Version)
		return accept("device", ref.ID, "report", e)
	}
	page := func(q Request) Page {
		t.Helper()
		r, e := s.Query(ctx, access.Principal{UserID: "admin"}, "one", q)
		if e != nil || r.Data == nil {
			t.Fatal(r, e)
		}
		return *r.Data
	}
	// AC-1: a command-only Run needs neither prompt, Context, Issue nor LLM.
	plain := create("plain", command("cli", "fixture-command"))
	var initial Spec
	_ = json.Unmarshal(read(head(plain).Input).Value, &initial)
	if len(initial.Input.Context) != 0 || len(initial.Input.Prompt) != 0 || head(plain).ExecutionRequestedAt != nil {
		t.Fatal("optional input was invented or execution requested")
	}
	for _, bad := range []Spec{{}, command("cli", "")} {
		status, _ := request("admin", "POST", path("bad", "create"), Create{Mutation: mutation("0"), Spec: bad})
		if status != 400 {
			t.Fatal("empty command accepted", status)
		}
	}
	status, _ := request("editor", "POST", path(plain.ID, "request-start"), mutation(plain.Version))
	if status != 403 {
		t.Fatal("edit permission granted execution", status)
	}
	// AC-2: input and derivation/definition versions remain fixed after updates.
	admin := access.Principal{UserID: "admin"}
	text := "selected original"
	putContext := func(kind, id, version string, value any) contexts.Ref {
		t.Helper()
		r, e := c.Put(ctx, admin, "one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: mutation(version), Value: raw(value)})
		if e != nil || r.Data == nil {
			t.Fatal(r, e)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: r.Data.Version}
	}
	unit := putContext("sentence", "selected", "0", contexts.Unit{Kind: "sentence", Text: &text})
	definition := putContext("sentence", "definition", "0", contexts.Unit{Kind: "sentence", Text: &text})
	manifest := putContext("manifest", "frozen-input", "0", contexts.ResolveRequest{RunID: "frozen", Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{unit}}})
	frozenSpec := command("connector", "fixed-observation")
	frozenSpec.Manifest = &manifest
	frozenSpec.Input.Command.Binding = map[string]string{"context": "payload"}
	frozenSpec.Sources = []Ref{contextRef(definition)}
	frozen := prepare(create("frozen", frozenSpec))
	inputBefore := read(head(frozen).Input)
	text = "updated source"
	_ = putContext("sentence", "selected", unit.Revision, contexts.Unit{Kind: "sentence", Text: &text})
	_ = putContext("sentence", "definition", definition.Revision, contexts.Unit{Kind: "sentence", Text: &text})
	if !bytes.Equal(inputBefore.Value, read(head(frozen).Input).Value) {
		t.Fatal("input/definition update rewrote accepted input")
	}
	fixedManifest, e := c.Read(ctx, admin, "one", manifest)
	if e != nil || !bytes.Contains(fixedManifest.Value, []byte("selected original")) {
		t.Fatal("accepted Manifest changed with source", e)
	}
	status, frozenExport := request("admin", "GET", path(frozen.ID, "export")+"?version="+frozen.Version, nil)
	if status != 200 || !bytes.Contains(frozenExport, []byte(`"type":"manifest"`)) || !bytes.Contains(frozenExport, []byte(manifest.Revision)) || !bytes.Contains(frozenExport, []byte("selected original")) {
		t.Fatal("input export lost fixed Manifest or content", status)
	}
	// AC-3/4/5/7/8: the fixed adapter emits two structured questions and ordered
	// output. It is test code; the product has no fake executor or CLI launcher.
	inspection := command("cli", "fixed-two-pause-command")
	inspection.Derivation = &Contract{Name: "custom-inspection", Version: "contract-v1", ResultSchema: raw(map[string]any{"type": "object", "required": []string{"decision"}, "properties": map[string]any{"decision": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "INCONCLUSIVE"}}}}), VerdictField: "decision"}
	run := prepare(create("two-pauses", inspection))
	run = report(run, Event{Sequence: "1", Type: "started"})
	run = report(run, Event{Sequence: "2", Type: "output", Channel: "stdout", Source: "fixed-command", Value: raw("before first question")})
	run = report(run, Event{Sequence: "3", Type: "pause", Pause: &PauseRequest{ID: "first", Reason: "need choice", Request: raw(map[string]any{"question": "first?", "choices": []string{"A", "B"}})}})
	pausedExportVersion := run
	code, pausedExport := request("admin", "GET", path(run.ID, "export")+"?version="+run.Version, nil)
	if code != 200 || !bytes.Contains(pausedExport, []byte(`"type":"complete"`)) {
		t.Fatal("paused execution could not be exported", code)
	}
	pausedVersion := run.Version
	status, _ = request("viewer", "POST", path(run.ID, "answer"), Answer{Mutation: mutation(pausedVersion), PauseID: "first", Value: raw("viewer")})
	if status != 403 {
		t.Fatal("viewer answered", status)
	}
	answers := []Answer{{Mutation: mutation(pausedVersion), PauseID: "first", Value: raw("A")}, {Mutation: mutation(pausedVersion), PauseID: "first", Value: raw("B")}}
	actors := []string{"editor", "second"}
	codes := make([]int, 2)
	responses := make([][]byte, 2)
	var workers sync.WaitGroup
	for i := range answers {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			codes[i], responses[i] = request(actors[i], "POST", path(run.ID, "answer"), answers[i])
		}(i)
	}
	workers.Wait()
	winner := -1
	for i, code := range codes {
		if code == 200 {
			if winner != -1 {
				t.Fatal("two answers accepted")
			}
			winner = i
		} else if code != 409 {
			t.Fatal("unexpected concurrent response", code, string(responses[i]))
		}
	}
	if winner < 0 {
		t.Fatal("no answer accepted")
	}
	var accepted apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(responses[winner], &accepted)
	run = Ref{ID: accepted.Data.ID, Version: accepted.Data.Version}
	runAfterAnswer := head(run)
	var firstPause Pause
	_ = json.Unmarshal(read(*runAfterAnswer.ActivePause).Value, &firstPause)
	if runAfterAnswer.State != "paused" || firstPause.State != "received" || firstPause.AnsweredBy != actors[winner] || firstPause.DeliveredAt != nil {
		t.Fatal("answer was treated as delivery/resume", firstPause)
	}
	status, b := request(actors[winner], "POST", path(run.ID, "answer"), answers[winner])
	var replay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(b, &replay)
	if status != 200 || replay.Data == nil || *replay.Data != *accepted.Data {
		t.Fatal("answer replay differs", status, string(b))
	}
	exec(`UPDATE access_members SET role='viewer' WHERE workspace_id='one' AND user_id=$1`, actors[winner])
	status, _ = request(actors[winner], "POST", path(run.ID, "answer"), answers[winner])
	if status != 403 {
		t.Fatal("revoked member replayed update", status)
	}
	exec(`UPDATE access_members SET role='editor' WHERE workspace_id='one' AND user_id=$1`, actors[winner])
	run = report(run, Event{Sequence: "4", Type: "delivered", PauseID: "first", ResponseID: firstPause.ResponseID})
	if head(run).State != "paused" {
		t.Fatal("delivery became resume")
	}
	run = report(run, Event{Sequence: "5", Type: "resumed", PauseID: "first", ResponseID: firstPause.ResponseID})
	run = report(run, Event{Sequence: "6", Type: "output", Channel: "stderr", Source: "fixed-command", Value: raw("partial after resume")})
	run = report(run, Event{Sequence: "7", Type: "pause", Pause: &PauseRequest{ID: "second", Reason: "need evidence", Request: raw("second?")}})
	status, _ = request("editor", "POST", path(run.ID, "answer"), Answer{Mutation: mutation(run.Version), PauseID: "first", Value: raw("stale")})
	if status != 409 {
		t.Fatal("old question accepted during new pause", status)
	}
	run = accept("second", run.ID, "answer", Answer{Mutation: mutation(run.Version), PauseID: "second", Value: raw(map[string]any{"evidence": "ready"})})
	var secondPause Pause
	_ = json.Unmarshal(read(*head(run).ActivePause).Value, &secondPause)
	run = report(run, Event{Sequence: "8", Type: "delivered", PauseID: "second", ResponseID: secondPause.ResponseID})
	run = report(run, Event{Sequence: "9", Type: "resumed", PauseID: "second", ResponseID: secondPause.ResponseID})
	status, _ = request("device", "POST", path(run.ID, "report"), Event{Mutation: mutation(run.Version), Sequence: "10", Type: "resumed", PauseID: "second", ResponseID: secondPause.ResponseID})
	if status != 409 {
		t.Fatal("duplicate resume accepted", status)
	}
	run = report(run, Event{Sequence: "10", Type: "output", Channel: "result", Source: "fixed-command", Value: raw(map[string]any{"decision": "FAIL", "binary": map[string]string{"encoding": "base64", "data": "AP8="}}), Artifacts: []contexts.Fixed{{URI: "https://example.test/artifact", Revision: "fixed-artifact-v1", Range: "output", Digest: strings.Repeat("a", 64), Delivery: "reference", State: "unfetched"}}})
	run = report(run, Event{Sequence: "11", Type: "unknown"})
	if head(run).Observation != "unknown" || head(run).State != "running" || head(run).ActivePause != nil {
		t.Fatal("disconnect fabricated pause or terminal state")
	}
	run = report(run, Event{Sequence: "12", Type: "known"})
	zero := 0
	run = report(run, Event{Sequence: "13", Type: "finished", ExitCode: &zero, Report: raw(map[string]string{"decision": "FAIL"})})
	final := head(run)
	if final.State != "succeeded" || final.Assessment == nil || final.Assessment.Verdict != "FAIL" || final.ReportedThrough != "13" {
		t.Fatal("process exit and inspection verdict conflated", final)
	}
	pauses := page(Request{RunID: run.ID, Version: run.Version, Kind: "pause", Limit: 1})
	if len(pauses.Items) != 1 || pauses.Next == "" {
		t.Fatal("pause pagination missing", pauses)
	}
	pauses2 := page(Request{RunID: run.ID, Version: run.Version, Kind: "pause", Limit: 1, After: pauses.Next})
	if len(pauses2.Items) != 1 || pauses2.Next != "" {
		t.Fatal(pauses2)
	}
	for _, rec := range append(pauses.Items, pauses2.Items...) {
		var pause Pause
		_ = json.Unmarshal(rec.Value, &pause)
		if pause.State != "resolved" || pause.AnsweredAt == nil || pause.DeliveredAt == nil || pause.ResumedAt == nil {
			t.Fatal("resolved pause was lost", pause)
		}
	}
	outputs := page(Request{RunID: run.ID, Version: run.Version, Kind: "output"})
	if len(outputs.Items) != 3 || !bytes.Contains(outputs.Items[0].Value, []byte("before first question")) {
		t.Fatal("raw output order lost", outputs)
	}
	if pausedExportVersion.Version == run.Version || !bytes.Contains(pausedExport, []byte(pausedExportVersion.Version)) {
		t.Fatal("export changed source or failed to retain its fixed snapshot")
	}
	beforeFinal := read(run)
	for i, target := range []string{"fixed-two-pause-command", "changed-command", "implementation-fix"} {
		spec := command("cli", target)
		spec.Previous = &run
		spec.Derivation = &Contract{Name: fmt.Sprint("custom-kind-", i), Version: "v1"}
		next := create(fmt.Sprint("rerun-", i), spec)
		if next.ID == run.ID || head(next).Previous == nil || *head(next).Previous != run {
			t.Fatal("rerun did not link a new Run")
		}
	}
	if !bytes.Equal(beforeFinal.Value, read(run).Value) {
		t.Fatal("rerun changed original terminal Run")
	}
	// Failure, empty successful output, HumanReview and custom command all share
	// the same record lifecycle; no derived name is a fixed product enumeration.
	for _, scenario := range []struct {
		id, kind string
		exit     int
		output   bool
	}{{"failed", "cli", 1, true}, {"empty", "connector", 0, false}, {"human", "human", 0, false}, {"custom", "custom-connection", 0, false}} {
		r := prepare(create(scenario.id, command(scenario.kind, "fixed-"+scenario.id)))
		r = report(r, Event{Sequence: "1", Type: "started"})
		seq := "2"
		if scenario.output {
			r = report(r, Event{Sequence: "2", Type: "output", Channel: "stderr", Source: scenario.id, Value: raw("failure detail")})
			seq = "3"
		}
		r = report(r, Event{Sequence: seq, Type: "finished", ExitCode: &scenario.exit})
		out := page(Request{RunID: r.ID, Kind: "output"})
		if (!scenario.output && len(out.Items) != 0) || (scenario.output && len(out.Items) != 1) {
			t.Fatal("output missing or invented")
		}
		if scenario.exit != 0 && head(r).State != "failed" {
			t.Fatal("failed process marked successful")
		}
	}
	observationSpec := command("connector", "fixed-ci-observation")
	observationSpec.Derivation = &Contract{Name: "CIObservationRun", Version: "v1"}
	observation := prepare(create("observation", observationSpec))
	observation = report(observation, Event{Sequence: "1", Type: "started"})
	observation = report(observation, Event{Sequence: "2", Type: "pause", Pause: &PauseRequest{ID: "ci", Reason: "wait for CI", Request: raw(map[string]string{"run_id": "external-ci-1"}), ResponseKind: "event"}})
	status, _ = request("editor", "POST", path(observation.ID, "answer"), Answer{Mutation: mutation(observation.Version), PauseID: "ci", Value: raw("not an observed event")})
	if status != 409 {
		t.Fatal("user forged an observed event", status)
	}
	observation = report(observation, Event{Sequence: "3", Type: "observed-response", PauseID: "ci", Source: "fixture-ci/external-ci-1", Value: raw(map[string]string{"state": "completed"})})
	var observedPause Pause
	_ = json.Unmarshal(read(*head(observation).ActivePause).Value, &observedPause)
	if observedPause.ResponseOrigin == nil || observedPause.ResponseOrigin.Kind != "event" || observedPause.AnsweredBy != "device:device" {
		t.Fatal("observed event disguised as member answer", observedPause)
	}
	observation = report(observation, Event{Sequence: "4", Type: "delivered", PauseID: "ci", ResponseID: observedPause.ResponseID})
	observation = report(observation, Event{Sequence: "5", Type: "resumed", PauseID: "ci", ResponseID: observedPause.ResponseID})
	_ = report(observation, Event{Sequence: "6", Type: "finished", ExitCode: &zero})
	cancel := prepare(create("cancel", command("cli", "fixed-cancel")))
	cancel = report(cancel, Event{Sequence: "1", Type: "started"})
	cancel = accept("admin", cancel.ID, "cancel", mutation(cancel.Version))
	if head(cancel).State != "running" || !head(cancel).CancelRequested {
		t.Fatal("cancel request treated as confirmation")
	}
	cancel = report(cancel, Event{Sequence: "2", Type: "cancelled"})
	if head(cancel).State != "cancelled" {
		t.Fatal("cancel confirmation missing")
	}
	// AC-10: an actual file contains the fixed input, all response stages, raw
	// output and lineage. Reads/export do not alter adapter or source state.
	status, exported := request("admin", "GET", path(run.ID, "export")+"?version="+run.Version, nil)
	if status != 200 {
		t.Fatal(status, string(exported))
	}
	file := t.TempDir() + "/run.ndjson"
	if os.WriteFile(file, exported, 0600) != nil {
		t.Fatal("export write")
	}
	fromDisk, _ := os.ReadFile(file)
	count := 0
	complete := false
	seenKinds := map[string]int{}
	for _, line := range bytes.Split(bytes.TrimSpace(fromDisk), []byte{'\n'}) {
		var frame ExportFrame
		if json.Unmarshal(line, &frame) != nil {
			t.Fatal("invalid export frame")
		}
		if frame.Type == "record" {
			count++
			seenKinds[frame.Record.Kind]++
			saved := read(frame.Record.Ref)
			canonical := func(b []byte) []byte {
				d := json.NewDecoder(bytes.NewReader(b))
				d.UseNumber()
				var value any
				if err := d.Decode(&value); err != nil {
					t.Fatal(err)
				}
				return raw(value)
			}
			if !bytes.Equal(canonical(saved.Value), canonical(frame.Record.Value)) {
				t.Fatal("export differs from saved record")
			}
		}
		if frame.Type == "complete" {
			complete = frame.Run != nil && *frame.Run == run
		}
	}
	if !bytes.Contains(fromDisk, []byte("unfetched")) || !bytes.Contains(fromDisk, []byte("AP8=")) {
		t.Fatal("binary output or unfetched artifact lost")
	}
	if !complete || count == 0 || seenKinds["pause"] < 6 || seenKinds["output"] != 3 || seenKinds["input"] != 1 || seenKinds["event"] != 13 || seenKinds["link"] != 3 {
		t.Fatal("incomplete execution export", count, seenKinds)
	}
	if bytes.Contains(fromDisk, []byte(deviceToken)) || bytes.Contains(fromDisk, []byte("fixture-generation")) {
		t.Fatal("device credential in export")
	}
	if !bytes.Equal(beforeFinal.Value, read(run).Value) {
		t.Fatal("reference/export modified execution")
	}
	// Current policy, another Workspace and bounded-none apply equally to reads.
	if _, e := s.Read(ctx, access.Principal{UserID: "outsider"}, "one", run); e == nil {
		t.Fatal("outsider read")
	}
	op, e := a.CreateOperation(ctx, admin, "one", "none", "read", true, nil, mutation("0"))
	if e != nil || op.Data == nil {
		t.Fatal(op, e)
	}
	none := access.Principal{UserID: "admin", OperationID: "none"}
	if _, e = s.Read(ctx, none, "one", run); e == nil {
		t.Fatal("none read")
	}
	nonePage, e := s.Query(ctx, none, "one", Request{Kind: "run"})
	if e != nil || nonePage.Data == nil || len(nonePage.Data.Items) != 0 {
		t.Fatal("none returned Run candidates", nonePage, e)
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, contextRef(unit).ID)
	if _, e = s.Read(ctx, admin, "one", frozen); e == nil {
		t.Fatal("private input dependency leaked")
	}
	status, _ = request("admin", "GET", path(frozen.ID, "export")+"?version="+frozen.Version, nil)
	if status != 403 {
		t.Fatal("private source exported", status)
	}
	exec(`UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id=$1`, contextRef(unit).ID)
	again, e := persistence.Open(ctx, database)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	restarted := New(again, access.New(again, key, nil), contexts.New(again, access.New(again, key, nil), contexts.DefaultLimits()))
	after, e := restarted.Read(ctx, admin, "one", run)
	if e != nil || !bytes.Equal(after.Value, beforeFinal.Value) {
		t.Fatal("restart changed fixed record", e)
	}
	exec(`UPDATE access_devices SET enabled=false WHERE id='device'`)
	status, _ = request("device", "POST", path(plain.ID, "report"), Event{Mutation: mutation(plain.Version), Sequence: "1", Type: "started"})
	if status != 403 {
		t.Fatal("revoked device reported", status)
	}
	testAssignments(t, s, server, key)
	t.Log("command-only, immutable input, two pauses/concurrent answers, separate delivery/resume, raw output, verdict/state, new-Run lineage, paged export, current policy and restart passed")
}
