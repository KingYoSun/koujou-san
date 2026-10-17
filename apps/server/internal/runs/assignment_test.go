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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

// Called from the owned PostgreSQL suite; never touches a developer database.
func testAssignments(t *testing.T, s *Service, server *httptest.Server, key []byte) {
	t.Helper()
	ctx := context.Background()
	db := s.Store.Database()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO access_workspaces VALUES('assignment','assignment','admin');
 INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('assignment','admin','admin',true),('assignment','editor','editor',true),('assignment','viewer','viewer',false);
 INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('assign-one','assignment','admin','one','one','first',true,true),('assign-two','assignment','editor','two','two','first',true,true)`)
	tokens := map[string]string{}
	for _, id := range []string{"assign-one", "assign-two"} {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte("device\x00" + id + "\x00first"))
		tokens[id] = "d." + id + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	var serial atomic.Int64
	mutation := func(v string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: fmt.Sprint("assignment-", serial.Add(1)), ExpectedVersion: v}
	}
	base := "/runs/workspaces/assignment"
	request := func(actor, method, path string, value any) (int, []byte) {
		t.Helper()
		var b io.Reader
		if value != nil {
			b = bytes.NewReader(raw(value))
		}
		req, _ := http.NewRequest(method, server.URL+path, b)
		if token, ok := tokens[actor]; ok {
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("X-Fixture-User", actor)
		}
		res, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, body
	}
	accept := func(actor, path string, value any) Ref {
		t.Helper()
		code, b := request(actor, "POST", path, value)
		var r apicontract.Response[apicontract.Reference]
		_ = json.Unmarshal(b, &r)
		if code != 200 || r.Data == nil {
			t.Fatalf("%s: %d %s", path, code, b)
		}
		return Ref{ID: r.Data.ID, Version: r.Data.Version}
	}
	head := func(id string) (Record, Run) {
		t.Helper()
		r, e := s.Read(ctx, access.Principal{UserID: "admin"}, "assignment", Ref{ID: runID(id), Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		var v Run
		_ = json.Unmarshal(r.Value, &v)
		return r, v
	}
	plain := func(id string) Ref {
		return accept("admin", base+"/executions/"+id+"/create", Create{Mutation: mutation("0"), Spec: Spec{Input: Input{Command: Command{Kind: "cli", Target: "fixture", Binding: map[string]string{"prompt": "stdin"}}, Prompt: raw("declared input")}, Artifacts: []contexts.Fixed{{URI: "https://example.test/artifact", Revision: "fixed", Range: "output", Digest: strings.Repeat("a", 64), Delivery: "reference", State: "unfetched"}}}})
	}
	c := Connection{Kind: "cli", Target: "fixture", Version: "fixture-v1", Environment: "windows-fixture", Subject: "cli-account-ref", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"pause"}, Users: []string{"admin", "editor"}, Repositories: []string{"project"}}
	contact := func(device string, conn Connection) Ref {
		t.Helper()
		old, e := runner(ctx, db, "assignment", device)
		v := "0"
		if e == nil {
			v = old.Version
		}
		return accept(device, base+"/runner", RunnerContact{Mutation: mutation(v), RunnerID: "runner-" + device, Connections: []Connection{conn}})
	}
	contact("assign-one", c)
	contact("assign-two", c)
	repo := &Repository{ID: "project", Commit: strings.Repeat("a", 40)}
	assign := func(ref Ref, device string) Assign {
		return Assign{Mutation: mutation(ref.Version), DeviceID: device, Repository: repo, Required: []string{"pause"}}
	}
	first := plain("one")
	reqs := []Assign{assign(first, "assign-one"), assign(first, "assign-two")}
	codes := make([]int, 2)
	responses := make([][]byte, 2)
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], responses[i] = request("admin", "POST", base+"/executions/one/assign", reqs[i])
		}(i)
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 200)) {
		t.Fatal("two assignments accepted", codes)
	}
	current, r := head(first.ID)
	a := r.Assignment
	if a == nil || a.Input != r.Input || a.RequestedBy != "admin" || a.Connection.Subject != "cli-account-ref" || a.Area == nil || r.StartedAt != nil {
		t.Fatal("assignment identity/preparation", r)
	}
	winner := 0
	if codes[1] == 200 {
		winner = 1
	}
	code, replayed := request("admin", "POST", base+"/executions/one/assign", reqs[winner])
	if code != 200 || !bytes.Equal(replayed, responses[winner]) {
		t.Fatal("assignment replay changed", code)
	}
	for range 2 {
		code, b := request("admin", "GET", base+"/records/"+first.ID+"?version=head", nil)
		var rec Record
		_ = json.Unmarshal(b, &rec)
		if code != 200 || rec.Ref != current.Ref {
			t.Fatal("competing clients see different current state")
		}
	}
	code, _ = request("viewer", "POST", base+"/executions/one/assign", assign(first, "assign-one"))
	if code != 403 {
		t.Fatal("membership granted execute", code)
	}
	code, _ = request("outsider", "POST", base+"/executions/one/assign", assign(first, "assign-one"))
	if code != 403 {
		t.Fatal("foreign user assigned", code)
	}
	second := plain("two")
	second = accept("editor", base+"/executions/two/assign", assign(second, "assign-one"))
	_, r2 := head(second.ID)
	if r2.Assignment.Area.ID == a.Area.ID || r2.Assignment.Area.Branch == a.Area.Branch {
		t.Fatal("Run areas reused")
	}
	code, _ = request("admin", "GET", base+"/executions/one/assigned-input?generation="+a.Generation, nil)
	if code != 403 {
		t.Fatal("user forged device", code)
	}
	code, _ = request(a.DeviceID, "GET", base+"/executions/two/assigned-input?generation=wrong", nil)
	if code != 403 {
		t.Fatal("old generation read input", code)
	}
	code, b := request(a.DeviceID, "GET", base+"/executions/one/assigned-input?generation="+a.Generation, nil)
	var selected AssignedInput
	_ = json.Unmarshal(b, &selected)
	if code != 200 || selected.Spec.Input.Command.Target != "fixture" || selected.Spec.Artifacts[0].State != "unfetched" || selected.Assignment.Input != r.Input {
		t.Fatal("fixed input not retained", code)
	}
	event := func(ref Ref, sequence, kind, generation string, area *Area) Event {
		return Event{Mutation: mutation(ref.Version), Sequence: sequence, Type: kind, Generation: generation, PreparedArea: area}
	}
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "1", "started", a.Generation, nil))
	if code != 409 {
		t.Fatal("started before preparation", code)
	}
	current.Ref = accept(a.DeviceID, base+"/executions/one/report", event(current.Ref, "1", "prepared", a.Generation, a.Area))
	_, ready := head(first.ID)
	if ready.Assignment.PreparedAt == nil || ready.StartedAt != nil {
		t.Fatal("ready means started")
	}
	current.Ref = accept(a.DeviceID, base+"/executions/one/report", event(current.Ref, "2", "started", a.Generation, nil))
	_, started := head(first.ID)
	if started.StartedAt == nil || started.State != "running" {
		t.Fatal("start not recorded")
	}
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "3", "known", "old", nil))
	if code != 403 {
		t.Fatal("old generation accepted", code)
	}
	current.Ref = accept(a.DeviceID, base+"/executions/one/report", event(current.Ref, "3", "unknown", a.Generation, nil))
	// Expiration does not redistribute or stop a running command. Renewing the
	// same generation requires a current Runner, policy and requester permission.
	exec(`UPDATE run_records SET payload=jsonb_set(payload,'{assignment,expires_at}',to_jsonb($3::text)) WHERE workspace_id='assignment' AND id=$1 AND version=$2`, current.ID, current.Version, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "4", "known", a.Generation, nil))
	if code != 403 {
		t.Fatal("expired generation accepted", code)
	}
	_, expired := head(first.ID)
	if expired.State != "running" || expired.Observation != "unknown" || expired.FinishedAt != nil || expired.Assignment.DeviceID != a.DeviceID {
		t.Fatal("lease implied termination or reassignment")
	}
	current.Ref = accept(a.DeviceID, base+"/executions/one/report", event(current.Ref, "4", "renew", a.Generation, nil))
	_, renewed := head(first.ID)
	if renewed.Observation != "unknown" {
		t.Fatal("renewal invented process observation")
	}
	denied := c
	denied.Users = []string{"editor"}
	contact(a.DeviceID, denied)
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "5", "known", a.Generation, nil))
	if code != 403 {
		t.Fatal("revoked local policy accepted", code)
	}
	contact(a.DeviceID, c)
	forbiddenConnection := c
	forbiddenConnection.Users = nil
	forbiddenConnection.Confirmed = false
	contact("assign-one", forbiddenConnection)
	deniedRun := plain("policy-denied")
	code, _ = request("admin", "POST", base+"/executions/"+deniedRun.ID+"/assign", assign(deniedRun, "assign-one"))
	if code != 403 {
		t.Fatal("unconfirmed capability bypassed local policy", code)
	}
	_, deniedHead := head(deniedRun.ID)
	if deniedHead.Assignment != nil {
		t.Fatal("policy denial assigned a Run")
	}
	// Capability and availability failures preserve queued state without an assignment.
	for _, reason := range []string{"capability_unconfirmed", "capability_missing", "binding_missing", "device_absent"} {
		conn := c
		switch reason {
		case "capability_unconfirmed":
			conn.Confirmed = false
		case "capability_missing":
			conn.Capabilities = nil
		case "binding_missing":
			conn.Bindings = nil
		}
		contact("assign-one", conn)
		if reason == "device_absent" {
			exec(`UPDATE run_runners SET payload=jsonb_set(payload,'{expires_at}',to_jsonb($1::text)) WHERE workspace_id='assignment' AND device_id='assign-one'`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
		}
		ref := plain(reason)
		accept("admin", base+"/executions/"+ref.ID+"/assign", assign(ref, "assign-one"))
		_, waiting := head(ref.ID)
		if waiting.Preparation != reason || waiting.Assignment != nil || waiting.StartedAt != nil {
			t.Fatal("capability invented", reason, waiting)
		}
	}
	contact("assign-one", c)
	admin := access.Principal{UserID: "admin"}
	sourceText := "fixed assignment Context"
	put := func(kind, id string, value any) contexts.Ref {
		out, e := s.Contexts.Put(ctx, admin, "assignment", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: mutation("0"), Value: raw(value)})
		if e != nil || out.Data == nil {
			t.Fatal("fixed assignment source", out, e)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: out.Data.Version}
	}
	source := put("sentence", "source", contexts.Unit{Kind: "sentence", Text: &sourceText})
	manifest := put("manifest", "for-assignment", contexts.ResolveRequest{RunID: "manifest-assigned", Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{source}}})
	spec := Spec{Manifest: &manifest, Input: Input{Command: Command{Kind: "cli", Target: "fixture", Binding: map[string]string{"context": "stdin"}}}}
	manifestRun := accept("admin", base+"/executions/manifest-assigned/create", Create{Mutation: mutation("0"), Spec: spec})
	manifestRun = accept("admin", base+"/executions/manifest-assigned/assign", assign(manifestRun, "assign-one"))
	_, manifestHead := head(manifestRun.ID)
	code, b = request("assign-one", "GET", base+"/executions/manifest-assigned/assigned-input?generation="+manifestHead.Assignment.Generation, nil)
	var fixed AssignedInput
	_ = json.Unmarshal(b, &fixed)
	if code != 200 || fixed.Manifest == nil || fixed.Manifest.Ref != manifest || !bytes.Contains(fixed.Manifest.Value, []byte(sourceText)) {
		t.Fatal("assignment lost fixed Manifest", code)
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='assignment' AND id='context:source'`)
	code, _ = request("assign-one", "GET", base+"/executions/manifest-assigned/assigned-input?generation="+manifestHead.Assignment.Generation, nil)
	if code != 403 {
		t.Fatal("fixed input permission revocation bypassed", code)
	}
	contact("assign-one", c)
	// #18: device-only queue/state transport, without GUI OAuth or raw inputs in the queue.
	code, b = request("assign-one", "GET", base+"/runner/self", nil)
	var self Runner
	_ = json.Unmarshal(b, &self)
	if code != 200 || self.DeviceID != "assign-one" || self.Version == "0" {
		t.Fatal("device Runner self", code)
	}
	code, _ = request("admin", "GET", base+"/runner/assignments", nil)
	if code != 403 {
		t.Fatal("user forged device queue", code)
	}
	code, b = request("assign-one", "GET", base+"/runner/assignments", nil)
	var queue RunnerQueue
	_ = json.Unmarshal(b, &queue)
	if code != 200 || len(queue.Items) == 0 || bytes.Contains(b, []byte("declared input")) {
		t.Fatal("queue missing or copied input", code)
	}
	_, secondValue := head(second.ID)
	second = accept("assign-one", base+"/executions/two/report", event(second, "1", "prepared", secondValue.Assignment.Generation, secondValue.Assignment.Area))
	second = accept("assign-one", base+"/executions/two/report", event(second, "2", "started", secondValue.Assignment.Generation, nil))
	pauseEvent := event(second, "3", "pause", secondValue.Assignment.Generation, nil)
	pauseEvent.Pause = &PauseRequest{ID: "transport", Reason: "fixed question", Request: raw("question?")}
	second = accept("assign-one", base+"/executions/two/report", pauseEvent)
	second = accept("admin", base+"/executions/two/answer", Answer{Mutation: mutation(second.Version), PauseID: "transport", Value: raw("accepted by API")})
	code, b = request("assign-one", "GET", base+"/executions/two/runner-state?generation="+secondValue.Assignment.Generation, nil)
	var observed RunnerState
	_ = json.Unmarshal(b, &observed)
	if code != 200 || observed.Run != second || observed.Pause == nil || observed.Pause.State != "received" || observed.Value.State != "paused" {
		t.Fatal("device lost accepted answer", code)
	}
	connectionReq := RunnerConnection{Mutation: mutation(second.Version), Generation: secondValue.Assignment.Generation, Unknown: true}
	connectionRef := accept("assign-one", base+"/executions/two/connection", connectionReq)
	code, b = request("assign-one", "GET", base+"/executions/two/runner-state?generation="+secondValue.Assignment.Generation, nil)
	var connectionState RunnerState
	_ = json.Unmarshal(b, &connectionState)
	if code != 200 || connectionState.Value.ReportedThrough != "3" || connectionState.Value.Observation != "unknown" || connectionState.Value.State != "paused" || connectionState.Run != connectionRef {
		t.Fatal("transport observation changed execution facts", code)
	}
	replayConnection := accept("assign-one", base+"/executions/two/connection", connectionReq)
	if replayConnection != connectionRef {
		t.Fatal("connection receipt changed")
	}
	code, _ = request("assign-one", "GET", base+"/executions/two/runner-state?generation=old", nil)
	if code != 403 {
		t.Fatal("stale Runner observed response", code)
	}
	// A lost final HTTP response can replay its receipt after the assignment
	// lease expires; it cannot append new facts to a completed Run.
	delivery := event(connectionRef, "4", "delivered", secondValue.Assignment.Generation, nil)
	delivery.PauseID, delivery.ResponseID = "transport", observed.Pause.ResponseID
	second = accept("assign-one", base+"/executions/two/report", delivery)
	resume := event(second, "5", "resumed", secondValue.Assignment.Generation, nil)
	resume.PauseID, resume.ResponseID = delivery.PauseID, delivery.ResponseID
	second = accept("assign-one", base+"/executions/two/report", resume)
	finish := event(second, "6", "finished", secondValue.Assignment.Generation, nil)
	zero := 0
	finish.ExitCode = &zero
	second = accept("assign-one", base+"/executions/two/report", finish)
	exec(`UPDATE run_records SET payload=jsonb_set(payload,'{assignment,expires_at}',to_jsonb($1::text)) WHERE workspace_id='assignment' AND id=$2 AND version=$3`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), second.ID, second.Version)
	if replay := accept("assign-one", base+"/executions/two/report", finish); replay != second {
		t.Fatal("terminal report receipt changed")
	}
	code, _ = request("assign-one", "POST", base+"/executions/two/report", event(second, "7", "known", secondValue.Assignment.Generation, nil))
	if code != 409 {
		t.Fatal("new report mutated terminal Run", code)
	}
	contact(a.DeviceID, c)
	exec(`UPDATE access_members SET can_execute=false WHERE workspace_id='assignment' AND user_id='admin'`)
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "5", "known", a.Generation, nil))
	if code != 403 {
		t.Fatal("revoked requester permission accepted", code)
	}
	exec(`UPDATE access_members SET can_execute=true WHERE workspace_id='assignment' AND user_id='admin'; UPDATE access_devices SET enabled=false WHERE workspace_id='assignment'`)
	code, _ = request(a.DeviceID, "POST", base+"/executions/one/report", event(current.Ref, "5", "known", a.Generation, nil))
	if code != 403 {
		t.Fatal("revoked device accepted", code)
	}
	_, ended := head(first.ID)
	if ended.State != "running" || ended.FinishedAt != nil {
		t.Fatal("permission revocation implied stopped")
	}
	t.Log("assignment: two authenticated devices converge; capability/current policy/generation enforced; fixed input, per-Run area, ready/start and expiry/stop distinct")
}
