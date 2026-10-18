package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkAutomationMCP(t *testing.T, s *Service, p access.Principal, ordinary string, reader, contextEditor, none *mcp.ClientSession,
	connect func(string) (*mcp.ClientSession, error), call func(*mcp.ClientSession, string, any, bool) []byte,
	request func(string, string, any, map[string]string) (*http.Response, []byte), site string) {
	t.Helper()
	ctx := context.Background()
	n := 0
	mutation := func(version string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: "automation-fixture-" + strings.Repeat("x", n), ExpectedVersion: version}
	}
	scope, e := s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "automation-issue"}, contexts.Write{Mutation: mutation("0"), Value: json.RawMessage(`{"kind":"issue","name":"Triage受入: 方針と手動固定"}`)})
	if e != nil || scope.Data == nil {
		t.Fatal(scope, e)
	}
	target := automation.Ref{ID: scope.Data.ID, Version: scope.Data.Version}
	newOperation := func(id, purpose string) *mcp.ClientSession {
		t.Helper()
		r, body := request("POST", site+"/mcp/control/workspaces/mcp-one/operations/"+id, map[string]any{"purpose": purpose, "operation_id": id, "expected_version": "0"}, map[string]string{"Authorization": "Bearer " + ordinary})
		var created struct {
			Handle string `json:"handle"`
		}
		_ = json.Unmarshal(body, &created)
		if r.StatusCode != 200 || created.Handle == "" {
			t.Fatal("triage operation purpose", r.StatusCode, string(body))
		}
		c, e := connect(created.Handle)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	editor := newOperation("triage-editor", "triage-edit")
	manager := newOperation("triage-manager", "triage-manage")
	policy := automation.Policy{Name: "Triage受入方針", Scopes: []string{"automation-issue"}, Levels: automation.DefaultLevels(), Criteria: "fixture criteria", Context: []automation.Ref{}}
	policyRaw, _ := json.Marshal(policy)
	write := automation.Write{Mutation: mutation("0"), Value: policyRaw}
	args := triageWrite{ID: "policy:ui-triage", Write: write}
	call(reader, "triage_set", args, true)
	call(contextEditor, "triage_set", args, true)
	body := call(editor, "triage_set", args, false)
	var saved apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(body, &saved)
	if saved.Data == nil {
		t.Fatal(string(body))
	}
	policyRef := automation.Ref{ID: saved.Data.ID, Version: saved.Data.Version}
	res, httpBody := request("POST", site+"/automation/workspaces/mcp-one/records/"+policyRef.ID, write, map[string]string{"Authorization": "Bearer " + ordinary})
	var replay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(httpBody, &replay)
	if res.StatusCode != 200 || replay.Data == nil || *replay.Data != *saved.Data {
		t.Fatal("triage HTTP/MCP replay differs", string(httpBody))
	}
	mcpBody := call(reader, "triage_get", policyRef, false)
	res, httpBody = request("GET", site+"/automation/workspaces/mcp-one/records/"+policyRef.ID+"?version="+policyRef.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
	var m, h automation.Record
	_ = json.Unmarshal(mcpBody, &m)
	_ = json.Unmarshal(httpBody, &h)
	if res.StatusCode != 200 || m.Ref != h.Ref || string(m.Value) != string(h.Value) {
		t.Fatal("triage fixed version differs")
	}
	call(none, "triage_get", policyRef, true)
	set := func(id string, v any, session *mcp.ClientSession) automation.Ref {
		t.Helper()
		raw, _ := json.Marshal(v)
		body := call(session, "triage_set", triageWrite{ID: id, Write: automation.Write{Mutation: mutation("0"), Value: raw}}, false)
		var r apicontract.Response[apicontract.Reference]
		_ = json.Unmarshal(body, &r)
		if r.Data == nil {
			t.Fatal(string(body))
		}
		return automation.Ref{ID: r.Data.ID, Version: r.Data.Version}
	}
	activation := automation.Activation{Policy: policyRef, Enabled: true, AutoApply: true}
	raw, _ := json.Marshal(activation)
	call(editor, "triage_set", triageWrite{ID: "activation:ui-triage", Write: automation.Write{Mutation: mutation("0"), Value: raw}}, true)
	set("activation:ui-triage", activation, manager)
	set("control:triage", automation.Control{Kind: "triage", Enabled: true, CanRead: true, CanCommit: true, Slots: 1, MaxAttempts: 3, LeaseSeconds: 30, TimeoutSeconds: 15, RetrySeconds: 1, MaxRequests: 100, PeriodSeconds: 3600}, manager)
	// A real shared Scope update feeds the transaction outbox; the supplied
	// deterministic executor is a test fixture, not a product model implementation.
	scope, e = s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "automation-issue"}, contexts.Write{Mutation: mutation(target.Version), Value: json.RawMessage(`{"kind":"issue","name":"Triage受入: 更新後"}`)})
	if e != nil || scope.Data == nil {
		t.Fatal(scope, e)
	}
	target.Version = scope.Data.Version
	level := "P1"
	worked, e := s.Automation.RunOne(ctx, "triage", "mcp-fixture-worker", func(context.Context, automation.Job) (automation.Result, error) {
		return automation.Result{Priority: &level, Reason: "受入fixtureの推奨", UsageState: "not_applicable"}, nil
	})
	if e != nil || !worked {
		t.Fatal(worked, e)
	}
	status, e := s.Automation.Priority(ctx, p, "mcp-one", target.ID)
	if e != nil {
		t.Fatal(e)
	}
	manual := "P2"
	edit := automation.PriorityEdit{Target: target, Policy: policyRef, Value: &manual, Reason: "受入fixtureの手動判断"}
	raw, _ = json.Marshal(edit)
	body = call(editor, "triage_set", triageWrite{ID: status.ID, Write: automation.Write{Mutation: mutation(status.Version), Value: raw}}, false)
	_ = json.Unmarshal(body, &saved)
	if saved.Data == nil {
		t.Fatal(string(body))
	}
	priorityRef := automation.Ref{ID: saved.Data.ID, Version: saved.Data.Version}
	// Keep the saved priority/recommendation on the old input while the current
	// Issue awaits evaluation, including the real Web/Tauri acceptance fixture.
	scope, e = s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "automation-issue"}, contexts.Write{Mutation: mutation(target.Version), Value: json.RawMessage(`{"kind":"issue","name":"Triage受入: 再評価待ちでも手動変更できる"}`)})
	if e != nil || scope.Data == nil {
		t.Fatal(scope, e)
	}
	target.Version = scope.Data.Version
	for _, q := range []automation.Request{{Mode: "heads", Kind: "policy", Limit: 1}, {Mode: "records", Kind: "priority", Limit: 50}, {Mode: "jobs", Limit: 50}, {Mode: "priority", ID: target.ID}, {Mode: "selection", ID: target.ID}} {
		body := call(reader, "triage_query", q, false)
		res, httpBody = request("POST", site+"/automation/workspaces/mcp-one/query", q, map[string]string{"Authorization": "Bearer " + ordinary})
		var m, h apicontract.Response[automation.Page]
		_ = json.Unmarshal(body, &m)
		_ = json.Unmarshal(httpBody, &h)
		if res.StatusCode != 200 || m.Data == nil || h.Data == nil || string(encodeTest(m.Data.Items)) != string(encodeTest(h.Data.Items)) {
			t.Fatal("triage query differs", q, string(body), string(httpBody))
		}
		if strings.Contains(string(body), `"lease":`) || strings.Contains(string(body), `"checkpoint":`) {
			t.Fatal("worker resume material exposed")
		}
	}
	if e = pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
		return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: target.ID, Restricted: true}, nil)
	}); e != nil {
		t.Fatal(e)
	}
	call(reader, "triage_get", priorityRef, true)
	body = call(reader, "triage_query", automation.Request{Mode: "jobs", Limit: 50}, false)
	var denied apicontract.Response[automation.Page]
	_ = json.Unmarshal(body, &denied)
	if denied.Data == nil || len(denied.Data.Items) != 0 {
		t.Fatal("private target leaked via jobs", string(body))
	}
	if e = pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
		return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: target.ID}, nil)
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Access.DB.Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); e != nil {
		t.Fatal(e)
	}
	call(manager, "triage_set", args, true)
	if _, e = s.Access.DB.Exec(ctx, `UPDATE access_members SET role='admin' WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); e != nil {
		t.Fatal(e)
	}

	batchWrite := triageWrite{ID: "batch:mcp-triage", Write: automation.Write{Mutation: mutation("0"), Value: encodeTest(automation.BatchEdit{Policy: policyRef, Scopes: []string{target.ID}, From: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour), Count: 1, PageSize: 1, Cost: &usage.CostBudget{Amount: "1", Currency: "USD", Valuation: "estimated"}})}}
	call(reader, "triage_set", batchWrite, true)
	call(contextEditor, "triage_set", batchWrite, true)
	body = call(manager, "triage_set", batchWrite, false)
	var batchSaved apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(body, &batchSaved)
	if batchSaved.Data == nil {
		t.Fatal("MCP batch write", string(body))
	}
	replayBody := call(manager, "triage_set", batchWrite, false)
	var batchReplay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(replayBody, &batchReplay)
	if batchReplay.Data == nil || batchSaved.Data.Version != batchReplay.Data.Version {
		t.Fatal("MCP batch replay")
	}
	for _, q := range []automation.Request{{Mode: "priorities", Limit: 1}, {Mode: "heads", Kind: "batch", Limit: 10}, {Mode: "records", Kind: "batch", Limit: 10}} {
		mcpBody := call(reader, "triage_query", q, false)
		res, httpBody := request("POST", site+"/automation/workspaces/mcp-one/query", q, map[string]string{"Authorization": "Bearer " + ordinary})
		var m, h apicontract.Response[automation.Page]
		_ = json.Unmarshal(mcpBody, &m)
		_ = json.Unmarshal(httpBody, &h)
		if res.StatusCode != 200 || m.Data == nil || h.Data == nil || string(encodeTest(m.Data.Items)) != string(encodeTest(h.Data.Items)) {
			t.Fatal("new triage query differs", q, string(mcpBody), string(httpBody))
		}
	}
	t.Log("real Ory + MCP/HTTP triage policy, activation boundary, manual fixed priority, current roles, private basis and bounded state passed")
}
func encodeTest(v any) []byte { raw, _ := json.Marshal(v); return raw }
