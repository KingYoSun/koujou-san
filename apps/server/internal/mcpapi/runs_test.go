package mcpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkRunsMCP(t *testing.T, s *Service, p access.Principal, reader, none *mcp.ClientSession,
	call func(*mcp.ClientSession, string, any, bool) []byte,
	request func(string, string, any, map[string]string) (*http.Response, []byte), site, ordinary string) {
	t.Helper()
	ctx := context.Background()
	serial := 0
	m := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("runs-fixture-", serial), ExpectedVersion: version}
	}
	if _, err := s.Access.DB.Exec(ctx, `UPDATE access_members SET can_execute=true WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); err != nil {
		t.Fatal(err)
	}
	// Enrol through the real authenticated user HTTP boundary, then give only this
	// fixture device report permission and an explicitly seeded test assignment.
	res, b := request("POST", site+"/access/workspaces/mcp-one/device-register", map[string]any{"id": "run-fixture-device", "name": "Run protocol fixture", "identifier": "run-fixture-device", "operation_id": "run-fixture-enrol", "expected_version": "0"}, map[string]string{"Authorization": "Bearer " + ordinary})
	var registered struct {
		Credential string `json:"credential"`
	}
	_ = json.Unmarshal(b, &registered)
	if res.StatusCode != 200 || registered.Credential == "" {
		t.Fatal("fixture device enrolment", res.StatusCode)
	}
	if _, err := s.Access.DB.Exec(ctx, `UPDATE access_devices SET can_input=true,can_report=true WHERE id='run-fixture-device'`); err != nil {
		t.Fatal(err)
	}
	device, err := s.Access.DevicePrincipal(ctx, "mcp-one", registered.Credential)
	if err != nil {
		t.Fatal(err)
	}
	create := func(id string) runs.Ref {
		t.Helper()
		spec := runs.Spec{Artifacts: []contexts.Fixed{{URI: "https://example.test/fixture-artifact", Revision: "fixed-artifact-v1", Range: "output", Digest: strings.Repeat("a", 64), Delivery: "reference", State: "unfetched"}}, Name: "Run受入: " + id, Input: runs.Input{Command: runs.Command{Kind: "human", Target: "fixture-question", Parameters: json.RawMessage(`{"request":"受入fixture。外部へ依頼を送信しない"}`)}}, Derivation: &runs.Contract{Name: "HumanReviewRun", Version: "fixed-v1", ResultSchema: json.RawMessage(`{"type":"object","required":["decision"]}`), VerdictField: "decision"}}
		created, e := s.Runs.Create(ctx, p, "mcp-one", id, runs.Create{Mutation: m("0"), Spec: spec})
		if e != nil || created.Data == nil {
			t.Fatal(created, e)
		}
		prepared, e := s.Runs.Action(ctx, p, "mcp-one", created.Data.ID, "request-start", m(created.Data.Version))
		if e != nil || prepared.Data == nil {
			t.Fatal(prepared, e)
		}
		if e = pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
			return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: created.Data.ID, AssignedDevice: "run-fixture-device"}, nil)
		}); e != nil {
			t.Fatal(e)
		}
		ref := runs.Ref{ID: prepared.Data.ID, Version: prepared.Data.Version}
		for _, event := range []runs.Event{{Sequence: "1", Type: "started"}, {Sequence: "2", Type: "output", Channel: "stdout", Source: "fixed-human-request", Value: json.RawMessage(`"Pause前の原出力"`)}, {Sequence: "3", Type: "pause", Pause: &runs.PauseRequest{ID: "question-one", Reason: "判断を待つ", Request: json.RawMessage(`{"question":"受入fixtureの確認","choices":["承認","差し戻し"]}`)}}} {
			event.Mutation = m(ref.Version)
			out, e := s.Runs.Report(ctx, device, "mcp-one", ref.ID, event)
			if e != nil || out.Data == nil {
				t.Fatal(out, e)
			}
			ref.Version = out.Data.Version
		}
		return ref
	}
	webRun := create("ui-run")
	nativeRun := create("ui-native-run")
	for _, ref := range []runs.Ref{webRun, nativeRun} {
		body := call(reader, "run_get", ref, false)
		res, httpBody := request("GET", site+"/runs/workspaces/mcp-one/records/"+ref.ID+"?version="+ref.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
		var a, b runs.Record
		_ = json.Unmarshal(body, &a)
		_ = json.Unmarshal(httpBody, &b)
		if res.StatusCode != 200 || a.Ref != b.Ref || string(a.Value) != string(b.Value) {
			t.Fatal("Run fixed HTTP/MCP versions differ")
		}
		call(none, "run_get", ref, true)
		for _, kind := range []string{"input", "pause", "output", "event", "all"} {
			q := runs.Request{RunID: ref.ID, Version: ref.Version, Kind: kind, Limit: 2}
			got := call(reader, "run_query", q, false)
			res, other := request("POST", site+"/runs/workspaces/mcp-one/query", q, map[string]string{"Authorization": "Bearer " + ordinary})
			var a, b apicontract.Response[runs.Page]
			_ = json.Unmarshal(got, &a)
			_ = json.Unmarshal(other, &b)
			if res.StatusCode != 200 || a.Data == nil || b.Data == nil || string(encodeTest(a.Data)) != string(encodeTest(b.Data)) {
				t.Fatal("Run page differs", kind)
			}
			if strings.Contains(string(got), registered.Credential) {
				t.Fatal("Run reply contains device credential")
			}
		}
	}
	var before, after int
	_ = s.Access.DB.QueryRow(ctx, "SELECT count(*) FROM run_records").Scan(&before)
	res, b = request("GET", site+"/runs/workspaces/mcp-one/executions/"+webRun.ID+"/export?version="+webRun.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
	if res.StatusCode != 200 || !strings.Contains(string(b), `"type":"complete"`) || strings.Contains(string(b), registered.Credential) {
		t.Fatal("Run export incomplete or unsafe", res.StatusCode)
	}
	_ = s.Access.DB.QueryRow(ctx, "SELECT count(*) FROM run_records").Scan(&after)
	if before != after {
		t.Fatal("Run reference/export performed a mutation")
	}
	if err = pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
		return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: webRun.ID, Restricted: true, AssignedDevice: "run-fixture-device"}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	call(reader, "run_get", webRun, true)
	q := runs.Request{Kind: "run", Limit: 50}
	got := call(reader, "run_query", q, false)
	var visible apicontract.Response[runs.Page]
	_ = json.Unmarshal(got, &visible)
	if visible.Data == nil {
		t.Fatal("missing Run page")
	}
	for _, v := range visible.Data.Items {
		if v.ID == webRun.ID {
			t.Fatal("private Run in page")
		}
	}
	if err = pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
		return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: webRun.ID, AssignedDevice: "run-fixture-device"}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Access.DB.Exec(ctx, `UPDATE access_members SET can_execute=false WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); err != nil {
		t.Fatal(err)
	}
	t.Log("real Ory + HTTP/MCP fixed Run/input/Pause/output/contract pages, none/private denial, streaming export and side-effect-free references passed; Web/native questions remain paused")
}
