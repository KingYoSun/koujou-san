package mcpapi

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/hopper"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
	"testing"
)

func checkHopperMCP(t *testing.T, s *Service, p access.Principal, ordinary string, reader, contextEditor, none *mcp.ClientSession, secret contexts.Ref, connect func(string) (*mcp.ClientSession, error), call func(*mcp.ClientSession, string, any, bool) []byte, request func(string, string, any, map[string]string) (*http.Response, []byte), site string) {
	t.Helper()
	ctx := context.Background()
	n := 0
	mutation := func(v string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: "hopper-mcp-" + strings.Repeat("x", n), ExpectedVersion: v}
	}
	template, err := s.Workflows.Put(ctx, p, "mcp-one", "run-definition:hopper-mcp", workflows.Write{Mutation: mutation("0"), Value: json.RawMessage(`{"schema":"run-definition-v1","name":"Hopper MCP","spec":{"input":{"command":{"kind":"cli","target":"fixture"}}}}`)})
	if err != nil || template.Data == nil {
		t.Fatal(template, err)
	}
	newOperation := func(id, purpose string) *mcp.ClientSession {
		t.Helper()
		r, b := request("POST", site+"/mcp/control/workspaces/mcp-one/operations/"+id, map[string]any{"purpose": purpose, "operation_id": id, "expected_version": "0"}, map[string]string{"Authorization": "Bearer " + ordinary})
		var made struct {
			Handle string `json:"handle"`
		}
		_ = json.Unmarshal(b, &made)
		if r.StatusCode != 200 || made.Handle == "" {
			t.Fatal(r.StatusCode, string(b))
		}
		c, err := connect(made.Handle)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	editor := newOperation("hopper-editor", "hopper-edit")
	manager := newOperation("hopper-manager", "hopper-manage")
	d := hopper.Template()
	d.Name = "Hopper受入: 保存と有効化"
	d.Problem = nil
	d.Targets = []hopper.Target{{Kind: "run", ID: "*"}}
	d.Condition = hopper.Condition{Field: "run.state", Op: "eq", Value: json.RawMessage(`"never"`)}
	d.Action.Template = hopper.Ref{ID: template.Data.ID, Version: template.Data.Version}
	value, _ := json.Marshal(d)
	write := hopper.Write{Mutation: mutation("0"), Value: value}
	args := hopperWrite{ID: "hopper:mcp", Write: write}
	call(reader, "hopper_set", args, true)
	call(contextEditor, "hopper_set", args, true)
	body := call(editor, "hopper_set", args, false)
	var saved apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(body, &saved)
	if saved.Data == nil {
		t.Fatal(string(body))
	}
	pin := hopper.Ref{ID: saved.Data.ID, Version: saved.Data.Version}
	res, b := request("POST", site+"/hopper/workspaces/mcp-one/records/"+pin.ID, write, map[string]string{"Authorization": "Bearer " + ordinary})
	var replay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(b, &replay)
	if res.StatusCode != 200 || replay.Data == nil || *saved.Data != *replay.Data {
		t.Fatal("HTTP/MCP replay mismatch", res.StatusCode, string(b))
	}
	mcpBody := call(reader, "hopper_get", pin, false)
	res, b = request("GET", site+"/hopper/workspaces/mcp-one/records/"+pin.ID+"?version="+pin.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
	var m, h hopper.Record
	_ = json.Unmarshal(mcpBody, &m)
	_ = json.Unmarshal(b, &h)
	if res.StatusCode != 200 || m.Ref != h.Ref || string(m.Value) != string(h.Value) {
		t.Fatal("fixed version mismatch")
	}
	call(none, "hopper_get", pin, true)
	active, _ := json.Marshal(hopper.Activation{Definition: pin, Enabled: true})
	activation := hopperWrite{ID: "hopper-active:mcp", Write: hopper.Write{Mutation: mutation("0"), Value: active}}
	call(editor, "hopper_set", activation, true)
	call(manager, "hopper_set", activation, false)
	// A template with a secret classification stays available to ordinary local
	// management, but its provenance cannot be exported through the MCP operation.
	privateDef := workflows.Definition{Schema: "run-definition-v1", Name: "private Hopper template", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture"}}, Sources: []runs.Ref{{ID: "context:" + secret.ID, Version: secret.Revision}}}}
	privateRaw, _ := json.Marshal(privateDef)
	tr, err := s.Workflows.Put(ctx, p, "mcp-one", "run-definition:hopper-private", workflows.Write{Mutation: mutation("0"), Value: privateRaw})
	if err != nil || tr.Data == nil {
		t.Fatal(tr, err)
	}
	d.Action.Template = hopper.Ref{ID: tr.Data.ID, Version: tr.Data.Version}
	value, _ = json.Marshal(d)
	secretRecord, err := s.Hopper.Put(ctx, p, "mcp-one", "hopper:private", hopper.Write{Mutation: mutation("0"), Value: value})
	if err != nil || secretRecord.Data == nil {
		t.Fatal(secretRecord, err)
	}
	privatePin := hopper.Ref{ID: secretRecord.Data.ID, Version: secretRecord.Data.Version}
	call(reader, "hopper_get", privatePin, true)
	var before, after int
	_ = s.Hopper.Store.Database().QueryRow(ctx, `SELECT count(*) FROM automation_jobs`).Scan(&before)
	page := call(reader, "hopper_query", hopper.Request{Mode: "heads", Kind: "definition", Limit: 50}, false)
	if strings.Contains(string(page), "hopper:private") {
		t.Fatal("private basis leaked", string(page))
	}
	call(reader, "hopper_get", pin, false)
	_ = s.Hopper.Store.Database().QueryRow(ctx, `SELECT count(*) FROM automation_jobs`).Scan(&after)
	if before != after {
		t.Fatal("read created work")
	}
	t.Log("Hopper HTTP/MCP: fixed version, replay, separate edit/manage, empty scope, private classification and read-only effects PASS")
}
