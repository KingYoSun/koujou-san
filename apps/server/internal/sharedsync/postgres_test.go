package sharedsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func legacyContext(ctx context.Context, t *testing.T, url string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "CREATE TABLE shared_migrations(version integer PRIMARY KEY,digest text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"001_shared.sql", "002_auth.sql", "003_access.sql", "004_context.sql"} {
		raw, err := os.ReadFile("../persistence/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if _, err = conn.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec(ctx, "INSERT INTO shared_migrations VALUES($1,$2)", i+1, hex.EncodeToString(sum[:])); err != nil {
			t.Fatal(err)
		}
	}
	_, err = conn.Exec(ctx, `INSERT INTO auth_users VALUES('legacy','fixture','legacy','legacy@koujou.test','legacy',false);
 INSERT INTO access_workspaces VALUES('legacy','legacy','legacy'); INSERT INTO access_members VALUES('legacy','legacy','admin',false,true);
 INSERT INTO shared_streams VALUES('legacy','origin','position-last',1);
 INSERT INTO shared_versions VALUES('legacy','context:legacy-s','r1',false);
 INSERT INTO context_records VALUES('legacy','context:legacy-s','sentence','r1','{"kind":"sentence","text":"legacy content"}');
 INSERT INTO shared_changes VALUES('legacy',1,'position-last','context:legacy-s','r1','context-sentence',false,'{}');`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:sync")
	}
	ctx := context.Background()
	legacyContext(ctx, t, database)
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var seq int64
	var legacy string
	if err = store.Database().QueryRow(ctx, "SELECT stream_sequence,payload->>'text' FROM context_records WHERE workspace_id='legacy'").Scan(&seq, &legacy); err != nil || seq != 1 || legacy != "legacy content" {
		t.Fatal("legacy Context migration did not preserve content/position")
	}
	for _, id := range []string{"alice", "bob", "outsider"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)", id); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("k", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if id != "alice" && id != "bob" && id != "outsider" {
			w.WriteHeader(401)
			return "", false
		}
		return id, true
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	s := New(store, a, c, []byte(strings.Repeat("s", 32)))
	mux := http.NewServeMux()
	a.Register(mux)
	c.Register(mux)
	s.Register(mux)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Drop-Response") == "1" {
			sink := httptest.NewRecorder()
			mux.ServeHTTP(sink, r)
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	request := func(actor, path string, body any, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(encode(body)))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			var safe map[string]any
			_ = json.Unmarshal(raw, &safe)
			delete(safe, "credential")
			delete(safe, "invitation")
			t.Fatalf("%s got%d want%d: %s", path, response.StatusCode, want, encode(safe))
		}
		return raw
	}
	accessPath := func(w, kind string) string { return "/access/workspaces/" + w + "/" + kind }
	contextPath := func(w, kind, id string) string { return "/contexts/workspaces/" + w + "/" + kind + "/" + id }
	pullPath := "/sync/workspaces/one/pull"
	counter := 0
	mutation := func(version string) apicontract.Mutation {
		counter++
		return apicontract.Mutation{OperationID: fmt.Sprintf("test-op-%d", counter), ExpectedVersion: version}
	}
	version := func(raw []byte) string {
		t.Helper()
		var response apicontract.Response[apicontract.Reference]
		if json.Unmarshal(raw, &response) != nil || response.Data == nil {
			t.Fatalf("missing reference: %s", raw)
		}
		return response.Data.Version
	}
	for _, w := range []string{"one", "two"} {
		request("alice", accessPath(w, "create"), map[string]any{"id": w, "name": w, "operation_id": "create-" + w, "expected_version": "0"}, 200)
	}
	_, _ = store.Database().Exec(ctx, "INSERT INTO access_members VALUES('one','bob','viewer',false,true)")
	write := func(actor, kind, id, previous string, value any) contexts.Ref {
		t.Helper()
		raw := request(actor, contextPath("one", kind, id), contexts.Write{Mutation: mutation(previous), Value: encode(value)}, 200)
		return contexts.Ref{Kind: kind, ID: id, Revision: version(raw)}
	}
	text := func(value string) contexts.Unit { return contexts.Unit{Kind: "sentence", Text: &value} }
	refs := []contexts.Ref{}
	for i := 0; i < 105; i++ {
		refs = append(refs, write("alice", "sentence", fmt.Sprintf("s%03d", i), "0", text(fmt.Sprintf("initial-%03d", i))))
	}
	scope := write("alice", "scope", "scope-a", "0", contexts.Scope{Kind: "issue", Name: "Sync scope"})
	membership := write("alice", "membership", "group-a", "0", contexts.Membership{Scope: contexts.Identity{Kind: "issue", ID: scope.ID}, Context: refs[0].Identity(), Present: true})
	pull := func(actor string, req Request) apicontract.Response[Frame] {
		t.Helper()
		raw := request(actor, pullPath, req, 200)
		var response apicontract.Response[Frame]
		if json.Unmarshal(raw, &response) != nil {
			t.Fatal("invalid frame")
		}
		return response
	}
	first := pull("bob", Request{Mode: "snapshot"})
	if first.Data == nil || len(first.Data.Items) != 100 || first.Meta.NextCursor == nil || first.Meta.Completeness.State != "partial" {
		t.Fatal("snapshot did not page")
	}
	if first.Data.Items[0].Editable {
		t.Fatal("viewer became editor")
	}
	known := []contexts.Identity{}
	for _, item := range first.Data.Items {
		known = append(known, item.Record.Ref.Identity())
	}
	newer := write("alice", "sentence", refs[0].ID, refs[0].Revision, text("updated after snapshot page"))
	second := pull("bob", Request{Mode: "snapshot", Cursor: *first.Meta.NextCursor})
	if second.Data == nil || second.Meta.NextCursor != nil || len(second.Data.Items) != 7 || *second.Meta.Position != *first.Meta.Position {
		t.Fatal("snapshot range moved during paging")
	}
	changes := pull("bob", Request{Mode: "changes", Position: *first.Meta.Position, Known: known})
	if changes.Data == nil || len(changes.Data.Items) != 1 || changes.Data.Items[0].Record.Ref.Revision != newer.Revision {
		t.Fatal("delta did not reconcile paged snapshot")
	}
	duplicate := pull("bob", Request{Mode: "changes", Position: *first.Meta.Position, Known: known})
	if duplicate.Data == nil || duplicate.Data.Items[0].Record.Ref.Revision != newer.Revision {
		t.Fatal("notification/retrieval replay changed revision")
	}
	// Recreate the API handlers to exercise cursor persistence across API restart.
	s = New(store, a, c, []byte(strings.Repeat("s", 32)))
	restart := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := http.NewServeMux()
		s.Register(m)
		m.ServeHTTP(w, r)
	}))
	defer restart.Close()
	restartReq, _ := http.NewRequest("POST", restart.URL+pullPath, bytes.NewReader(encode(Request{Mode: "changes", Position: *first.Meta.Position, Known: known})))
	restartReq.Header.Set("Authorization", "Bearer bob")
	restartResponse, err := http.DefaultClient.Do(restartReq)
	if err != nil {
		t.Fatal(err)
	}
	var restarted apicontract.Response[Frame]
	_ = json.NewDecoder(restartResponse.Body).Decode(&restarted)
	_ = restartResponse.Body.Close()
	if restarted.Data == nil || restarted.Data.Items[0].Record.Ref.Revision != newer.Revision {
		t.Fatal("API restart lost update/cursor")
	}
	cross := pull("alice", Request{Mode: "snapshot", Cursor: *first.Meta.NextCursor})
	if cross.Data != nil || cross.Error == nil {
		t.Fatal("cursor reused by another User")
	}
	request("outsider", pullPath, Request{Mode: "snapshot"}, 403)
	request("bob", "/sync/workspaces/two/pull", Request{Mode: "snapshot"}, 403)
	t.Log("AC-1: fixed paged snapshot, update and repeated/delayed retrieval, cursor binding and API restart passed")

	bobVersion := version(request("alice", accessPath("one", "member-update"), map[string]any{"operation_id": "bob-editor", "expected_version": "0", "id": "bob", "role": "editor", "active": true}, 200))
	same := newer
	var before int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM shared_changes WHERE workspace_id='one'").Scan(&before)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i, actor := range []string{"alice", "bob"} {
		body := contexts.Write{Mutation: mutation(same.Revision), Value: encode(text(fmt.Sprintf("concurrent-%d", i)))}
		wg.Add(1)
		go func(actor string, body contexts.Write) {
			defer wg.Done()
			req, _ := http.NewRequest("POST", server.URL+contextPath("one", "sentence", same.ID), bytes.NewReader(encode(body)))
			req.Header.Set("Authorization", "Bearer "+actor)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			_ = response.Body.Close()
			statuses <- response.StatusCode
		}(actor, body)
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent status counts: %+v", counts)
	}
	var current string
	_ = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='one' AND resource_id='context:s000'").Scan(&current)
	reapplied := write("bob", "sentence", same.ID, current, text("manually reconciled"))
	var after int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM shared_changes WHERE workspace_id='one'").Scan(&after)
	if after-before != 2 {
		t.Fatal("concurrent request silently overwrote")
	}
	groupPayload := contexts.Membership{Scope: contexts.Identity{Kind: "issue", ID: scope.ID}, Context: refs[0].Identity(), Present: false}
	grouped := write("bob", "membership", membership.ID, membership.Revision, groupPayload)
	request("alice", contextPath("one", "membership", membership.ID), contexts.Write{Mutation: mutation(membership.Revision), Value: encode(groupPayload)}, 409)
	grouped = write("alice", "membership", grouped.ID, grouped.Revision, contexts.Membership{Scope: groupPayload.Scope, Context: groupPayload.Context, Present: true})
	t.Log("AC-2: Context concurrent CAS and explicit reconciliation, membership conflict/reapply passed")

	loss := contexts.Write{Mutation: mutation(reapplied.Revision), Value: encode(text("response deliberately lost"))}
	lost, _ := http.NewRequest("POST", server.URL+contextPath("one", "sentence", same.ID), bytes.NewReader(encode(loss)))
	lost.Header.Set("Authorization", "Bearer alice")
	lost.Header.Set("X-Test-Drop-Response", "1")
	if response, err := http.DefaultClient.Do(lost); err == nil {
		_ = response.Body.Close()
		t.Fatal("test did not lose response")
	}
	receipt := request("alice", contextPath("one", "sentence", same.ID), loss, 200)
	again := request("alice", contextPath("one", "sentence", same.ID), loss, 200)
	if !bytes.Equal(receipt, again) {
		t.Fatal("response-loss resend changed receipt")
	}
	var receiptCount int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM shared_receipts WHERE workspace_id='one' AND actor_id='user:alice' AND operation_id=$1", loss.OperationID).Scan(&receiptCount)
	if receiptCount != 1 {
		t.Fatal("duplicate receipt after response loss")
	}
	t.Log("AC-3: accepted response loss, same-operation resend and single committed result passed")

	_, _ = store.Database().Exec(ctx, "DELETE FROM shared_changes WHERE workspace_id='one' AND sequence<=(SELECT sequence FROM shared_changes WHERE workspace_id='one' AND position=$1)", *first.Meta.Position)
	history := pull("bob", Request{Mode: "changes", Position: *first.Meta.Position, Known: known})
	if history.Error == nil || history.Meta.Completeness.Reason != "history_unavailable" {
		t.Fatal("missing history became a complete delta")
	}
	restored := pull("bob", Request{Mode: "snapshot"})
	if restored.Data == nil || len(restored.Data.Items) != 100 {
		t.Fatal("snapshot could not recover without old journal")
	}
	if restored.Data.Items[0].Record.Ref.Revision != version(receipt) {
		t.Fatal("snapshot did not recover current Context")
	}
	t.Log("AC-5: journal loss rejects completeness and snapshot recovers immutable record heads")

	registered := request("alice", accessPath("one", "device-register"), map[string]any{"operation_id": "register-device", "expected_version": "0", "id": "device-a", "identifier": "physical-a", "name": "fixture device"}, 200)
	var private map[string]json.RawMessage
	_ = json.Unmarshal(registered, &private)
	var credential string
	_ = json.Unmarshal(private["credential"], &credential)
	var registration struct {
		Result apicontract.Response[apicontract.Reference] `json:"result"`
	}
	_ = json.Unmarshal(registered, &registration)
	deviceVersion := version(request("alice", accessPath("one", "device-update"), map[string]any{"operation_id": "allow-reports", "expected_version": registration.Result.Data.Version, "id": "device-a", "active": true, "can_report": true}, 200))
	err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: "run:fixture", AssignedDevice: "device-a"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	event := ReportInput{ID: "event-1", TargetID: "run:fixture", Sequence: "1", Kind: "output_observed", ArtifactID: "artifact-a", ArtifactRevision: "a1"}
	reportPath := "/sync/workspaces/one/reports"
	ack := request(credential, reportPath, event, 200)
	if !bytes.Equal(ack, request(credential, reportPath, event, 200)) {
		t.Fatal("report duplicate changed accepted result")
	}
	event3 := ReportInput{ID: "event-3", TargetID: event.TargetID, Sequence: "3", Kind: "completion_observed"}
	request(credential, reportPath, event3, 200)
	streamID := topic(event.TargetID, "device-a")
	record, err := s.readReport(ctx, access.Principal{UserID: "bob"}, "one", streamID, "")
	if err != nil {
		t.Fatal(err)
	}
	var summary ReportSummary
	_ = json.Unmarshal(record.Value, &summary)
	if !summary.Missing || summary.Contiguous != "1" || !summary.CompletionObserved || summary.ResultState != "unconfirmed" {
		t.Fatal("missing report became a successful result")
	}
	event2 := ReportInput{ID: "event-2", TargetID: event.TargetID, Sequence: "2", Kind: "pause_observed"}
	request(credential, reportPath, event2, 200)
	complete, err := s.readReport(ctx, access.Principal{UserID: "bob"}, "one", streamID, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(complete.Value, &summary)
	if summary.Missing || summary.Contiguous != "3" || summary.ResultState != "unconfirmed" {
		t.Fatal("late report not reconciled independently of real result")
	}
	prior, err := s.readReport(ctx, access.Principal{UserID: "bob"}, "one", streamID, record.Ref.Revision)
	if err != nil || !bytes.Equal(prior.Value, record.Value) {
		t.Fatal("historical report state changed")
	}
	duplicateSeq := event2
	duplicateSeq.ID = "different-event-2"
	request(credential, reportPath, duplicateSeq, 409)
	malicious := map[string]any{"event_id": "bad-event", "target_id": event.TargetID, "sequence": "4", "kind": "output_observed", "credential": "fixture-secret-must-not-sync"}
	request(credential, reportPath, malicious, 400)
	pathEvent := event
	pathEvent.ID = "path-event"
	pathEvent.Sequence = "4"
	pathEvent.ArtifactID = `C:\private\log`
	request(credential, reportPath, pathEvent, 400)
	pathRevision := event
	pathRevision.ID = "path-revision-event"
	pathRevision.Sequence = "4"
	pathRevision.ArtifactRevision = `C:\private\log`
	request(credential, reportPath, pathRevision, 400)
	otherTarget := event
	otherTarget.ID = "wrong-target"
	otherTarget.Sequence = "4"
	otherTarget.TargetID = "run:other"
	request(credential, reportPath, otherTarget, 403)
	request("alice", reportPath, event, 403)
	beforePolicy := pull("bob", Request{Mode: "snapshot"})
	position := *beforePolicy.Meta.Position
	err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: event.TargetID, Restricted: true, AssignedDevice: "device-a"}, []access.ResourceGrant{{UserID: "alice", Read: true}})
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := pull("bob", Request{Mode: "changes", Position: position, Known: []contexts.Identity{{Kind: "report", ID: streamID}}})
	if policy.Data == nil || len(policy.Data.Removed) != 1 {
		t.Fatal("permission-only change allowed cache reuse")
	}
	request("alice", accessPath("one", "device-update"), map[string]any{"operation_id": "revoke-device", "expected_version": deviceVersion, "id": "device-a", "active": false}, 200)
	request(credential, reportPath, event, 403)
	request("alice", accessPath("one", "member-update"), map[string]any{"operation_id": "revoke-bob", "expected_version": bobVersion, "id": "bob", "role": "editor", "active": false}, 200)
	request("bob", pullPath, Request{Mode: "changes", Position: position}, 403)
	var leaked bool
	_ = store.Database().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shared_changes WHERE payload::text LIKE '%fixture-secret-must-not-sync%')").Scan(&leaked)
	if leaked {
		t.Fatal("unknown private report fields entered journal")
	}
	t.Log("AC-6: Device-only assigned reports, 1/3/2 gaps/late delivery, duplicates, immutable summary, current revocation and secret/path rejection passed")
}
