package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkWorkflowsMCP(t *testing.T, s *Service, p access.Principal, reader, none, outside *mcp.ClientSession, call func(*mcp.ClientSession, string, any, bool) []byte, request func(string, string, any, map[string]string) (*http.Response, []byte), site, ordinary string) {
	t.Helper()
	ctx := context.Background()
	w := "mcp-one"
	must := func(v apicontract.Response[apicontract.Reference], err error) workflows.Ref {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatal(v, err)
		}
		return workflows.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	definition := workflows.Definition{Schema: "run-definition-v1", Name: "受入: 工程command", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture", Parameters: json.RawMessage(`{"exact":123456789012345678901234567890}`)}}}}
	ref := must(s.Workflows.Put(ctx, p, w, "run-definition:ui-command", workflows.Write{Mutation: apicontract.Mutation{OperationID: "workflow-ui-command", ExpectedVersion: "0"}, Value: encodeTest(definition)}))
	graph := workflows.Definition{Schema: "workflow-definition-v1", Name: "受入: 手動工程", Budget: &workflows.Budget{Parallel: 1, MaxRuns: 1}, Nodes: []workflows.Node{{ID: "command", Definition: &ref}}}
	workflow := must(s.Workflows.Put(ctx, p, w, "workflow-definition:ui-workflow", workflows.Write{Mutation: apicontract.Mutation{OperationID: "workflow-ui-graph", ExpectedVersion: "0"}, Value: encodeTest(graph)}))
	var before, after int
	_ = s.Access.DB.QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&before)
	for _, fixed := range []workflows.Ref{ref, workflow} {
		body := call(reader, "workflow_get", fixed, false)
		res, httpBody := request("GET", site+"/workflows/workspaces/"+w+"/records/"+fixed.ID+"?version="+fixed.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
		var a, b workflows.Record
		_ = json.Unmarshal(body, &a)
		_ = json.Unmarshal(httpBody, &b)
		if res.StatusCode != 200 || a.Ref != b.Ref || string(a.Value) != string(b.Value) {
			t.Fatal("workflow fixed HTTP/MCP differs")
		}
		body = call(reader, "workflow_validate", fixed, false)
		res, httpBody = request("POST", site+"/workflows/workspaces/"+w+"/validate", fixed, map[string]string{"Authorization": "Bearer " + ordinary})
		var av, bv workflows.Validation
		_ = json.Unmarshal(body, &av)
		_ = json.Unmarshal(httpBody, &bv)
		if res.StatusCode != 200 || !av.Valid || string(encodeTest(av)) != string(encodeTest(bv)) {
			t.Fatal("workflow validation HTTP/MCP differs", string(body))
		}
		call(none, "workflow_get", fixed, true)
		call(none, "workflow_validate", fixed, true)
		call(outside, "workflow_get", fixed, true)
		res, _ = request("GET", site+"/workflows/workspaces/mcp-two/records/"+fixed.ID+"?version="+fixed.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
		if res.StatusCode != 403 {
			t.Fatal("foreign workflow ref granted")
		}
	}
	res, body := request("POST", site+"/access/workspaces/mcp-one/device-register", map[string]any{"id": "workflow-fixture", "name": "workflow fixture", "identifier": "workflow-fixture", "operation_id": "workflow-device", "expected_version": "0"}, map[string]string{"Authorization": "Bearer " + ordinary})
	var enrolled struct {
		Credential string `json:"credential"`
	}
	_ = json.Unmarshal(body, &enrolled)
	if res.StatusCode != 200 {
		t.Fatal("workflow capability device", res.StatusCode)
	}
	device, err := s.Access.DevicePrincipal(ctx, w, enrolled.Credential)
	if err != nil {
		t.Fatal(err)
	}
	contact := must(s.Runs.ContactRunner(ctx, device, w, runs.RunnerContact{Mutation: apicontract.Mutation{OperationID: "workflow-contact", ExpectedVersion: "0"}, RunnerID: "workflow-fixture", Connections: []runs.Connection{{Kind: "cli", Target: "fixture", Version: "v1", Environment: "fixture", Subject: "fixture", Confirmed: true, Users: []string{p.UserID}}}}))
	args := map[string]string{"device": "workflow-fixture", "version": contact.Version}
	body = call(reader, "workflow_capabilities", args, false)
	res, other := request("GET", site+"/workflows/workspaces/"+w+"/capabilities/workflow-fixture?version="+contact.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
	var a, b runs.Runner
	_ = json.Unmarshal(body, &a)
	_ = json.Unmarshal(other, &b)
	if res.StatusCode != 200 || string(encodeTest(a)) != string(encodeTest(b)) {
		t.Fatal("capability HTTP/MCP differs")
	}
	call(none, "workflow_capabilities", args, true)
	_ = s.Access.DB.QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&after)
	if before != after {
		t.Fatal("workflow read/validate performed execution")
	}
	t.Log("HTTP/MCP fixed workflow definitions, static validation and capabilities match; none/outside/foreign reads denied and Run writes 0")
}
