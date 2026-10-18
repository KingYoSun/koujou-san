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
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/searchindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type selectionTransport struct {
	base   http.RoundTripper
	client string
}

func (t selectionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("X-Koujou-Client", t.client)
	return t.base.RoundTrip(r)
}
func checkClientSelectionMCP(t *testing.T, s *Service, p access.Principal, apiToken, mcpToken string, transport http.RoundTripper, reader, contextEditor, none *mcp.ClientSession, public, secret contexts.Ref, call func(*mcp.ClientSession, string, any, bool) []byte, request func(string, string, any, map[string]string) (*http.Response, []byte), site string) {
	t.Helper()
	ctx := context.Background()
	// Client bookkeeping must not turn unchanged search pages into pending data
	// or invalidate their continuation, even when projection catches up mid-loop.
	if err := s.Search.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.Search.Query(ctx, p, "mcp-one", searchindex.Request{Limit: 2})
	if err != nil || first.Meta.NextCursor == nil {
		t.Fatal("search continuation fixture", err)
	}
	policyProbe, _ := json.Marshal(clientops.DefaultPolicy())
	probe, err := s.ClientOps.PutPolicy(ctx, p, "mcp-one", "selection-policy:pagination-probe", clientops.Write{Mutation: apicontract.Mutation{OperationID: "pagination-probe", ExpectedVersion: "0"}, Value: policyProbe})
	if err != nil || probe.Error != nil {
		t.Fatal("bookkeeping fixture", err)
	}
	page, err := s.Search.Query(ctx, p, "mcp-one", searchindex.Request{Limit: 2})
	if err != nil || page.Meta.Completeness.Reason == "projection_pending" {
		t.Fatal("bookkeeping falsely makes candidates pending", err)
	}
	if err = s.Search.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = s.Search.Query(ctx, p, "mcp-one", searchindex.Request{Limit: 2, Cursor: *first.Meta.NextCursor})
	if err != nil || page.Error != nil {
		t.Fatal("bookkeeping invalidates unchanged continuation", err, page.Error)
	}
	n := 0
	m := func(v string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: fmt.Sprint("selection-mcp-", n), ExpectedVersion: v}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) clientops.Ref {
		t.Helper()
		if err != nil || v.Error != nil || v.Data == nil {
			t.Fatalf("selection fixture %+v %v", v.Error, err)
		}
		return clientops.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	connect := func(handle, client string) (*mcp.ClientSession, error) {
		return mcp.NewClient(&mcp.Implementation{Name: "selection-acceptance", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: site + "/mcp", HTTPClient: &http.Client{Transport: selectionTransport{headerTransport{transport, mcpToken, "mcp-one", handle}, client}}}, nil)
	}
	newHandle := func(id, purpose string) string {
		must(s.Access.CreateOperation(ctx, p, "mcp-one", id, purpose, false, nil, m("0")))
		return s.Access.OperationHandle("mcp-one", id, p.UserID)
	}
	editor, err := connect(newHandle("selection-editor", "selection-edit"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer editor.Close()
	policy := clientops.DefaultPolicy()
	policy.Fixed.Mode = "none"
	b, _ := json.Marshal(policy)
	args := policyWrite{ID: "selection-policy:mcp", Write: clientops.Write{Mutation: m("0"), Value: b}}
	call(reader, "selection_policy_set", args, true)
	call(contextEditor, "selection_policy_set", args, true)
	result := call(editor, "selection_policy_set", args, false)
	var made apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(result, &made)
	if made.Data == nil {
		t.Fatal(string(result))
	}
	r, body := request("POST", site+"/client-operations/workspaces/mcp-one/policies/"+args.ID, args.Write, map[string]string{"Authorization": "Bearer " + apiToken})
	var replay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(body, &replay)
	if r.StatusCode != 200 || replay.Data == nil || *replay.Data != *made.Data {
		t.Fatal("HTTP/MCP policy replay differs", r.StatusCode)
	}
	ref := clientops.Ref{ID: made.Data.ID, Version: made.Data.Version}
	call(reader, "selection_get", ref, false)
	call(none, "selection_get", ref, true)
	write := selectionWrite{ID: "client-selection:mcp", Write: clientops.SelectionWrite{Mutation: m("0"), Input: contexts.Selection{Mode: "none"}}}
	call(reader, "selection_set", write, true)
	result = call(editor, "selection_set", write, false)
	_ = json.Unmarshal(result, &made)
	if made.Data == nil {
		t.Fatal(string(result))
	}
	r, body = request("POST", site+"/client-operations/workspaces/mcp-one/selections/"+write.ID, write.Write, map[string]string{"Authorization": "Bearer " + apiToken})
	_ = json.Unmarshal(body, &replay)
	if r.StatusCode != 200 || replay.Data == nil || *replay.Data != *made.Data {
		t.Fatal("HTTP/MCP selection replay differs", r.StatusCode)
	}
	// Owned selection transports expose only read tools and keep excluded IDs out of compact previews.
	paragraph := must(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "paragraph", ID: "selection-composite"}, contexts.Write{Mutation: m("0"), Value: json.RawMessage(fmt.Sprintf(`{"kind":"paragraph","children":[%s,%s]}`, mustJSON(public), mustJSON(secret)))}))
	root := contexts.Ref{Kind: "paragraph", ID: "selection-composite", Revision: paragraph.Version}
	policy.Fixed = contexts.Selection{Mode: "select", Include: []contexts.Ref{root}, Exclude: []contexts.Ref{secret}}
	b, _ = json.Marshal(policy)
	policyRef := must(s.ClientOps.PutPolicy(ctx, p, "mcp-one", "selection-policy:client-bound", clientops.Write{Mutation: m("0"), Value: b}))
	opRef := must(s.ClientOps.Start(ctx, p, "mcp-one", "client-operation:mcp", clientops.Start{Mutation: m("0"), Policy: policyRef, Target: clientops.Ref{ID: "client-selection:client-bound", Version: "0"}, Goal: "owned selection", Purpose: "inspection", Client: "owned-client", Model: clientops.Model{Connection: "owned", Revision: "1", Requested: "owned", Allowed: []string{"owned"}}}))
	opRecord, err := s.ClientOps.Read(ctx, p, "mcp-one", opRef)
	if err != nil {
		t.Fatal(err)
	}
	var op clientops.Operation
	_ = json.Unmarshal(opRecord.Value, &op)
	handle := s.Access.OperationHandle("mcp-one", op.ReadOperation, p.UserID)
	peer, err := connect(handle, "owned-client")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	call(peer, "context_get", secret, true)
	call(peer, "context_get", root, true)
	call(peer, "context_get", public, false)
	call(peer, "selection_set", write, true)
	call(peer, "run_query", map[string]any{"limit": 1}, true)
	preview := call(peer, "context_resolve", resolveRequest{Selection: contexts.Selection{Mode: "select", Include: []contexts.Ref{root}}}, false)
	if strings.Contains(string(preview), `"secret"`) || strings.Contains(string(preview), `"input_utf8"`) {
		t.Fatal("excluded metadata or full input reached model preview")
	}
	var checked struct {
		Data clientops.ModelResolution `json:"data"`
	}
	_ = json.Unmarshal(preview, &checked)
	if !checked.Data.Ready || len(checked.Data.Placements) != 2 {
		t.Fatal("compact preview missing fixed placements", string(preview))
	}
	if bad, err := connect(handle, "different-client"); err == nil {
		_ = bad.Close()
		t.Fatal("another client used the owned transport")
	}

	template, err := s.Workflows.Read(ctx, p, "mcp-one", workflows.Ref{ID: "run-definition:ui-command", Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := s.Workflows.Capabilities(ctx, p, "mcp-one", "workflow-fixture", "head")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"none", "select"} {
		policy.Fixed.Mode = mode
		policy.Definition = &clientops.DefinitionPolicy{Sources: []clientops.Ref{template.Ref, capability.Ref}, FixedPaths: []string{}}
		if mode == "none" {
			policy.Fixed = contexts.Selection{Mode: "none"}
		}
		b, _ = json.Marshal(policy)
		ref := must(s.ClientOps.PutPolicy(ctx, p, "mcp-one", "definition-policy:mcp-"+mode, clientops.Write{Mutation: m("0"), Value: b}))
		opRef := must(s.ClientOps.Start(ctx, p, "mcp-one", "client-operation:definition-"+mode, clientops.Start{Mutation: m("0"), Policy: ref, Target: clientops.Ref{ID: "run-definition:mcp-generated-" + mode, Version: "0"}, Purpose: "definition", Goal: "Generate without execution", Client: "owned-client", Model: clientops.Model{Connection: "owned", Revision: "1", Requested: "owned", Allowed: []string{"owned"}}}))
		load := func(ref clientops.Ref) clientops.Operation {
			r, err := s.ClientOps.Read(ctx, p, "mcp-one", ref)
			if err != nil {
				t.Fatal(err)
			}
			var op clientops.Operation
			json.Unmarshal(r.Value, &op)
			return op
		}
		op := load(opRef)
		handle := s.Access.OperationHandle("mcp-one", op.ReadOperation, p.UserID)
		peer, err := connect(handle, "owned-client")
		if err != nil {
			t.Fatal(err)
		}
		body := call(peer, "workflow_get", template.Ref, false)
		if !strings.Contains(string(body), "metadata_only") || strings.Contains(string(body), "parameters") {
			t.Fatal("template body leaked", string(body))
		}
		call(peer, "workflow_query", workflows.Request{Kind: "heads", Type: "run-definition", Limit: 1}, false)
		capBody := call(peer, "workflow_capabilities", map[string]string{"device": "workflow-fixture", "version": capability.Version}, false)
		var capReply struct {
			Meta apicontract.Meta `json:"meta"`
		}
		if json.Unmarshal(capBody, &capReply) != nil || capReply.Meta.Completeness.State != "complete" {
			t.Fatal("capability completeness missing", string(capBody))
		}
		call(peer, "workflow_get", workflows.Ref{ID: "workflow-definition:ui-workflow", Version: template.Version}, true)
		call(peer, "workflow_validate", template.Ref, true)
		call(peer, "context_get", secret, true)
		call(peer, "context_get", public, mode == "none")
		call(peer, "run_query", map[string]any{"limit": 1}, true)
		peer.Close()
		peer, err = connect(handle, "owned-client")
		if err != nil {
			t.Fatal("definition reconnect", err)
		}
		call(peer, "workflow_get", template.Ref, false)
		peer.Close()
		opRef = must(s.ClientOps.Act(ctx, p, "mcp-one", opRef.ID, clientops.Action{Mutation: m(opRef.Version), Kind: "takeover", Client: "next-client"}))
		if old, err := connect(handle, "owned-client"); err == nil {
			old.Close()
			t.Fatal("old definition owner client accepted")
		}
		peer, err = connect(handle, "next-client")
		if err != nil {
			t.Fatal("definition handoff", err)
		}
		call(peer, "workflow_get", template.Ref, false)
		peer.Close()
	}
	t.Log("client selection: HTTP/MCP fixed versions/replay, purpose separation, owned-client transport and no excluded metadata passed")
}
func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func checkClientGraphPagination(t *testing.T, s *Service, p access.Principal, root contexts.Ref) {
	t.Helper()
	ctx := context.Background()
	if err := s.Graph.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	graphPage, err := s.Graph.Query(ctx, p, "mcp-one", graphindex.Request{Root: root, Limit: 1})
	if err != nil || graphPage.Meta.NextCursor == nil {
		t.Fatal("graph continuation fixture", err, graphPage.Meta)
	}
	probe, _ := json.Marshal(clientops.DefaultPolicy())
	saved, err := s.ClientOps.PutPolicy(ctx, p, "mcp-one", "selection-policy:graph-pagination-probe", clientops.Write{Mutation: apicontract.Mutation{OperationID: "graph-pagination-probe", ExpectedVersion: "0"}, Value: probe})
	if err != nil || saved.Error != nil {
		t.Fatal("graph bookkeeping fixture", err)
	}
	graphPending, err := s.Graph.Query(ctx, p, "mcp-one", graphindex.Request{Root: root, Limit: 1})
	if err != nil || graphPending.Meta.Completeness.Reason == "projection_pending" {
		t.Fatal("bookkeeping falsely makes graph pending", err)
	}
	if err := s.Graph.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	graphPending, err = s.Graph.Query(ctx, p, "mcp-one", graphindex.Request{Root: root, Limit: 1, Cursor: *graphPage.Meta.NextCursor})
	if err != nil || graphPending.Error != nil {
		t.Fatal("bookkeeping invalidates graph continuation", err, graphPending.Error)
	}

}
