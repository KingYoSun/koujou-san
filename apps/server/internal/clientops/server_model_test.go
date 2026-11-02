package clientops

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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modeltest"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
)

func checkServerModel(t *testing.T, s *Service) {
	ctx := context.Background()
	p := access.Principal{UserID: "alice"}
	var calls atomic.Int32
	var mode atomic.Value
	mode.Store("complete")
	var flightOperation atomic.Value
	entered, release := make(chan struct{}, 1), make(chan struct{}, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := calls.Add(1) == 1
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer owned-server-key" || r.Header.Get("Cookie") != "" {
			t.Error("credential or endpoint boundary")
		}
		input, _ := io.ReadAll(r.Body)
		if first {
			entered <- struct{}{}
			<-release
		}
		switch mode.Load().(string) {
		case "tool":
			if !strings.Contains(string(input), `"type":"namespace"`) {
				t.Error("namespace missing")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: "+`{"type":"response.completed","response":{"id":"owned-tool","model":"owned-model","status":"completed","output":[{"type":"reasoning","id":"rs_owned","encrypted_content":"OPAQUE_REASONING","summary":[]},{"type":"function_call","id":"fc_owned","call_id":"call_owned","namespace":"koujou","name":"context_search","arguments":"{\"query\":\"owned\"}"}],"usage":{"input_tokens":12,"output_tokens":4}}}`+"\n\n")
			return
		case "round2":
			if !strings.Contains(string(input), "OPAQUE_REASONING") || !strings.Contains(string(input), "function_call_output") {
				t.Error("tool history lost")
			}
		case "incomplete", "failed", "wrong-incomplete":
			state := mode.Load().(string)
			actual := "owned-model"
			if state == "wrong-incomplete" {
				state = "incomplete"
				actual = "forbidden-model"
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"type\":\"response.%s\",\"response\":{\"id\":\"owned-partial\",\"model\":\"%s\",\"status\":\"%s\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Partial text\"}]}],\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}}\n\n", state, actual, state)
			return
		case "disconnect":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"response.created\"}\n\n")
			return
		case "revoke":
			if _, err := s.Store.Database().Exec(ctx, `UPDATE access_members SET active=false WHERE workspace_id='one' AND user_id='alice'`); err != nil {
				t.Error(err)
			}
		case "takeover":
			id := flightOperation.Load().(string)
			r, err := s.Read(ctx, p, "one", Ref{ID: id, Version: "head"})
			if err != nil {
				t.Error(err)
			} else {
				out, err := s.Act(ctx, p, "one", id, Action{Mutation: apicontract.Mutation{OperationID: "server-takeover:" + id, ExpectedVersion: r.Version}, Client: "new-client", Kind: "takeover"})
				if err != nil || out.Error != nil {
					t.Error("takeover failed", err, out.Error)
				}
			}
		}
		modeltest.Completed(w, "Owned server answer", "ORIGINAL_STREAM_MARKER")
	}))
	defer model.Close()
	previousGateway := s.Gateway
	t.Cleanup(func() { s.Gateway = previousGateway })
	config := filepath.Join(t.TempDir(), "connections.json")
	s.Gateway = modeltest.Plan(t, config, model.URL, "owned-server-key")
	connections, err := s.Gateway.Connections("one")
	if err != nil || len(connections) != 1 {
		t.Fatal(err)
	}
	connection := connections[0]
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("server-model-%d", serial), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		r, e := accepted(v, err)
		if e != nil {
			t.Fatalf("mutation %v %v", v.Error, err)
		}
		return r
	}
	project := must(s.Contexts.Put(ctx, p, "one", contexts.Identity{Kind: "scope", ID: "server-chat-project"}, contexts.Write{Mutation: mutation("0"), Value: raw(contexts.Scope{Kind: "project", Name: "Server model chat"})}))
	thread := must(s.PutChatThread(ctx, p, "one", "chat-thread:server", Write{Mutation: mutation("0"), Value: raw(ChatThread{Name: "Server chat", Project: project})}))
	human := must(s.PutChatMessage(ctx, p, "one", "chat-message:server-human", Write{Mutation: mutation("0"), Value: raw(ChatMessage{Thread: thread, Role: "human", Text: "OWNED_FIXED_INPUT", State: "complete"})}))
	policy := DefaultPolicy()
	policy.Mode = "proposal"
	policy.Fixed.Mode = "none"
	policy.Chat = &ChatSource{Thread: thread, Messages: []Ref{human}}
	policyRef := must(s.PutPolicy(ctx, p, "one", "chat-policy:server", Write{Mutation: mutation("0"), Value: raw(policy)}))
	body := raw(map[string]any{"model": "owned-model", "messages": []any{map[string]string{"role": "user", "content": "OWNED_FIXED_INPUT"}}, "stream": false})
	newOperation := func(label string) (Ref, serverModelRequest) {
		t.Helper()
		sum := sha256.Sum256(body)
		model := Model{Connection: "server:" + connection.ID + ":owned-model", ServerConnection: connection.ID, Revision: connection.Revision, Requested: "owned-model", Allowed: []string{"owned-model"}}
		operation := must(s.Start(ctx, p, "one", "client-operation:server-"+label, Start{Mutation: mutation("0"), Policy: policyRef, Target: thread, Goal: "Owned server chat", Purpose: "chat", Model: model, Client: "client"}))
		sending := must(s.Begin(ctx, p, "one", operation.ID, Begin{Mutation: mutation(operation.Version), Client: "client", InputDigest: hex.EncodeToString(sum[:]), PromptVersion: "project-chat-v1"}))
		r, err := s.Read(ctx, p, "one", sending)
		var op Operation
		if err != nil || decode(r.Value, &op) != nil || op.Turn == nil {
			t.Fatal(err)
		}
		return sending, serverModelRequest{Client: "client", Turn: *op.Turn, Body: body}
	}
	operation, request := newOperation("normal")
	for _, other := range []access.Principal{{UserID: "bob"}, {UserID: "other"}} {
		if _, err := s.ServerModel(ctx, other, "one", operation.ID, request); err == nil {
			t.Fatal("other user dispatched")
		}
	}
	if _, err := s.ServerModel(ctx, p, "two", operation.ID, request); err == nil {
		t.Fatal("other Workspace dispatched")
	}
	tampered := request
	tampered.Body = raw(map[string]string{"model": "unregistered"})
	if _, err := s.ServerModel(ctx, p, "one", operation.ID, tampered); err == nil {
		t.Fatal("tampered input dispatched")
	}
	if out, err := s.Finish(ctx, p, "one", operation.ID, Finish{Mutation: mutation(operation.Version), Client: "client", Turn: request.Turn, Response: raw(map[string]string{"model": "owned-model"})}); err == nil && out.Error == nil {
		t.Fatal("browser forged server result")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid requests reached upstream")
	}
	done := make(chan error, 1)
	go func() { _, err := s.ServerModel(ctx, p, "one", operation.ID, request); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal("dispatch rejected before upstream", err)
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not start")
	}
	if _, err := s.ServerModel(ctx, p, "one", operation.ID, request); err == nil {
		t.Fatal("in-flight request was replayed")
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal("dispatch failed", err)
	}
	if _, err := s.ServerModel(ctx, p, "one", operation.ID, request); err != nil {
		t.Fatal("lost ACK could not read saved result", err)
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate dispatch", calls.Load())
	}
	r, err := s.Read(ctx, p, "one", Ref{ID: operation.ID, Version: "head"})
	var op Operation
	if err != nil || decode(r.Value, &op) != nil || op.State != "completed" || op.Applied == nil {
		t.Fatal("server answer not adopted", op.State, op.Reason, err)
	}
	var answer ChatMessage
	a, err := s.Read(ctx, p, "one", *op.Applied)
	if err != nil || decode(a.Value, &answer) != nil || answer.Text != "Owned server answer" {
		t.Fatal("chat answer", err)
	}
	dispatchID := "client-server-dispatch:" + request.Turn.ID
	fixed, err := s.Read(ctx, p, "one", Ref{ID: dispatchID, Version: "head"})
	if err != nil || !strings.Contains(string(fixed.Value), "ORIGINAL_STREAM_MARKER") || !strings.Contains(string(fixed.Value), "OWNED_FIXED_INPUT") {
		t.Fatal("original request/response lost", err)
	}
	page, err := s.Query(ctx, p, "one", Request{Kind: "server-model-dispatch", Root: operation.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 || strings.Contains(string(page.Items[0].Value), "ORIGINAL_STREAM_MARKER") || strings.Contains(string(page.Items[0].Value), "OWNED_FIXED_INPUT") {
		t.Fatal("list returned model body", err)
	}
	var count int
	var protocol, quantity string
	err = s.Store.Database().QueryRow(ctx, `SELECT count(*),max(payload->'measurement'->>'protocol'),max(payload->'quantities'->'input'->>'value') FROM usage_records u JOIN shared_versions h ON h.workspace_id=u.workspace_id AND h.resource_id=u.id AND h.version=u.version WHERE u.workspace_id='one' AND u.kind='usage' AND u.payload->'attribution'->>'id'=$1`, operation.ID).Scan(&count, &protocol, &quantity)
	if err != nil || count != 1 || protocol != "openai-responses" || quantity != "23" {
		t.Fatal("usage", count, protocol, quantity, err)
	}
	stale, staleReq := newOperation("stale")
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(original), "Owned plan", "Changed connection", 1)
	if err := os.WriteFile(config, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ServerModel(ctx, p, "one", stale.ID, staleReq); err == nil {
		t.Fatal("changed connection dispatched")
	}
	if err := os.WriteFile(config, original, 0600); err != nil {
		t.Fatal(err)
	}
	denied, deniedReq := newOperation("delivery-denied")
	previous, version, err := s.Access.DeliveryPolicy(ctx, p, "one")
	if err != nil {
		t.Fatal(err)
	}
	blocked := previous
	blocked.DeniedScopes = append(append([]string{}, previous.DeniedScopes...), "server-chat-project")
	newPolicy := must(s.Access.SetDeliveryPolicy(ctx, p, "one", mutation(version), blocked))
	if _, err := s.ServerModel(ctx, p, "one", denied.ID, deniedReq); err == nil {
		t.Fatal("delivery-prohibited Project dispatched")
	}
	must(s.Access.SetDeliveryPolicy(ctx, p, "one", mutation(newPolicy.Version), previous))
	if calls.Load() != 1 {
		t.Fatal("blocked request reached upstream")
	}
	changedModel, changedModelReq := newOperation("changed-model-before-send")
	otherModel := Model{Connection: "direct-model", Revision: "direct-version", Requested: "owned-model", Allowed: []string{"owned-model"}}
	must(s.Act(ctx, p, "one", changedModel.ID, Action{Mutation: mutation(changedModel.Version), Client: "client", Kind: "takeover", Model: &otherModel}))
	if _, err := s.ServerModel(ctx, p, "one", changedModel.ID, changedModelReq); err == nil {
		t.Fatal("old turn accepted after model change")
	}
	if calls.Load() != 1 {
		t.Fatal("model change reached old upstream before rejection")
	}
	mode.Store("takeover")
	taken, takenReq := newOperation("takeover-in-flight")
	flightOperation.Store(taken.ID)
	if _, err := s.ServerModel(ctx, p, "one", taken.ID, takenReq); err == nil {
		t.Fatal("old client adopted after takeover")
	}
	r, err = s.Read(ctx, p, "one", Ref{ID: taken.ID, Version: "head"})
	op = Operation{}
	if err != nil || decode(r.Value, &op) != nil || op.Client != "new-client" || op.Applied != nil {
		t.Fatal("takeover boundary", err)
	}
	for _, state := range []string{"incomplete", "failed", "disconnect", "wrong-incomplete"} {
		mode.Store(state)
		operation, req := newOperation(state)
		if _, err := s.ServerModel(ctx, p, "one", operation.ID, req); err != nil {
			t.Fatal("terminal result not retained", state, err)
		}
		r, err := s.Read(ctx, p, "one", Ref{ID: operation.ID, Version: "head"})
		var op Operation
		if err != nil || decode(r.Value, &op) != nil || op.State == "completed" || op.State == "ready" {
			t.Fatal("non-completed response adopted", state, op.State, err)
		}
		if state == "incomplete" && op.State != "incomplete" {
			t.Fatal("incomplete chat state lost")
		}
		if state != "incomplete" && (op.State != "failed" || op.Applied != nil) {
			t.Fatal("failed response adopted", state, op.State)
		}
	}
	mode.Store("tool")
	previousBody, previousPolicy := body, policyRef
	toolPolicy := policy
	toolPolicy.Fixed.Mode = "select"
	toolPolicy.Scopes = []string{"server-chat-project"}
	policyRef = must(s.PutPolicy(ctx, p, "one", "chat-policy:server-tools", Write{Mutation: mutation("0"), Value: raw(toolPolicy)}))
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "context_search", "description": "Owned search", "parameters": map[string]string{"type": "object"}}}}
	body = raw(map[string]any{"model": "owned-model", "messages": []any{map[string]string{"role": "user", "content": "OWNED_FIXED_INPUT"}}, "tools": tools, "stream": false})
	toolOperation, toolReq := newOperation("tools")
	toolReply, err := s.ServerModel(ctx, p, "one", toolOperation.ID, toolReq)
	if err != nil {
		t.Fatal("tool dispatch", err)
	}
	var result struct {
		Choices []struct{ Message map[string]any }
	}
	if json.Unmarshal(toolReply.Body, &result) != nil || len(result.Choices) != 1 {
		t.Fatal("tool reply")
	}
	assistant := result.Choices[0].Message
	if assistant["role"] != "assistant" {
		t.Fatal("server reply lost assistant role")
	}
	body = raw(map[string]any{"model": "owned-model", "messages": []any{map[string]string{"role": "user", "content": "OWNED_FIXED_INPUT"}, assistant, map[string]string{"role": "tool", "tool_call_id": "call_owned", "content": "{\"items\":[]}"}}, "tools": tools, "stream": false})
	r, err = s.Read(ctx, p, "one", Ref{ID: toolOperation.ID, Version: "head"})
	if err != nil || decode(r.Value, &op) != nil || op.State != "ready" {
		t.Fatal("tool response did not continue", op.State, err)
	}
	toolSum := sha256.Sum256(body)
	next := must(s.Begin(ctx, p, "one", toolOperation.ID, Begin{Mutation: mutation(r.Version), Client: "client", InputDigest: hex.EncodeToString(toolSum[:]), PromptVersion: "project-chat-v1"}))
	r, err = s.Read(ctx, p, "one", next)
	if err != nil || decode(r.Value, &op) != nil {
		t.Fatal(err)
	}
	mode.Store("round2")
	if _, err := s.ServerModel(ctx, p, "one", toolOperation.ID, serverModelRequest{Client: "client", Turn: *op.Turn, Body: body}); err != nil {
		t.Fatal("tool return", err)
	}
	r, err = s.Read(ctx, p, "one", Ref{ID: toolOperation.ID, Version: "head"})
	if err != nil || decode(r.Value, &op) != nil || op.State != "completed" {
		t.Fatal("tool completion", op.State, err)
	}
	body, policyRef = previousBody, previousPolicy
	mode.Store("revoke")
	revoked, revokedReq := newOperation("revoked-in-flight")
	if _, err := s.ServerModel(ctx, p, "one", revoked.ID, revokedReq); err == nil {
		t.Fatal("revoked caller adopted response")
	}
	if _, err := s.Store.Database().Exec(ctx, `UPDATE access_members SET active=true WHERE workspace_id='one' AND user_id='alice'`); err != nil {
		t.Fatal(err)
	}
	retained, err := s.Read(ctx, p, "one", Ref{ID: "client-server-dispatch:" + revokedReq.Turn.ID, Version: "head"})
	if err != nil || !strings.Contains(string(retained.Value), "ORIGINAL_STREAM_MARKER") {
		t.Fatal("revocation discarded provider response", err)
	}
	r, err = s.Read(ctx, p, "one", Ref{ID: revoked.ID, Version: "head"})
	op = Operation{}
	if err != nil || decode(r.Value, &op) != nil || op.Applied != nil {
		t.Fatal("revoked response adopted", err)
	}
	var legacyCalls atomic.Int32
	var selectionMode atomic.Bool
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("x-bf-vk") != "owned-gateway-key" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("gateway credential boundary")
		}
		w.Header().Set("Content-Type", "application/json")
		content := "Owned gateway answer"
		if selectionMode.Load() {
			content = string(raw(Proposal{Items: []Item{}, Questions: []string{}, Unknown: []string{}}))
			input, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(input), "response_format") {
				t.Error("structured format missing")
			}
		}
		w.Write(raw(map[string]any{"id": "owned-gateway-result", "model": "owned-model", "choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 7, "completion_tokens": 3}}))
	}))
	defer legacy.Close()
	keyFile := filepath.Join(t.TempDir(), "gateway-key")
	if err := os.WriteFile(keyFile, []byte("owned-gateway-key"), 0600); err != nil {
		t.Fatal(err)
	}
	value := raw([]any{map[string]any{"id": "legacy", "name": "Owned gateway", "endpoint": legacy.URL + "/v1", "key_file": keyFile, "workspaces": []string{"one"}, "models": []serverllm.Model{{Requested: "owned-model", Actual: []string{"owned-model"}}}}})
	if err := os.WriteFile(config, value, 0600); err != nil {
		t.Fatal(err)
	}
	s.Gateway = &serverllm.Gateway{ConfigFile: config}
	listed, err := s.Gateway.Connections("one")
	if err != nil || len(listed) != 1 {
		t.Fatal(err)
	}
	connection = listed[0]
	legacyOperation, legacyReq := newOperation("legacy-gateway")
	mux := http.NewServeMux()
	s.Register(mux)
	recorder := httptest.NewRecorder()
	httpRequest := httptest.NewRequest("POST", "/client-operations/workspaces/one/operations/"+legacyOperation.ID+"/model", bytes.NewReader(raw(legacyReq)))
	httpRequest.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(recorder, httpRequest)
	if recorder.Code != 200 {
		t.Fatal("gateway HTTP route", recorder.Code, recorder.Body.String())
	}
	r, err = s.Read(ctx, p, "one", Ref{ID: legacyOperation.ID, Version: "head"})
	op = Operation{}
	if err != nil || decode(r.Value, &op) != nil || op.State != "completed" || legacyCalls.Load() != 1 {
		t.Fatal("gateway result", op.State, err)
	}
	selectionMode.Store(true)
	selectionPolicy := DefaultPolicy()
	selectionPolicy.Fixed.Mode = "none"
	selectionPolicyRef := must(s.PutPolicy(ctx, p, "one", "selection-policy:server-owned", Write{Mutation: mutation("0"), Value: raw(selectionPolicy)}))
	var runsBefore int
	if err := s.Store.Database().QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&runsBefore); err != nil {
		t.Fatal(err)
	}
	selection := must(s.Start(ctx, p, "one", "client-operation:server-selection", Start{Mutation: mutation("0"), Policy: selectionPolicyRef, Target: Ref{ID: "client-selection:server-owned", Version: "0"}, Purpose: "selection", Goal: "Owned selection", Model: Model{Connection: "server:legacy:owned-model", ServerConnection: "legacy", Revision: connection.Revision, Requested: "owned-model", Allowed: []string{"owned-model"}}, Client: "client"}))
	selectionBody := raw(map[string]any{"model": "owned-model", "messages": []any{map[string]string{"role": "user", "content": "Owned selection"}}, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "context_selection", "strict": true, "schema": map[string]string{"type": "object"}}}})
	selectionSum := sha256.Sum256(selectionBody)
	started := must(s.Begin(ctx, p, "one", selection.ID, Begin{Mutation: mutation(selection.Version), Client: "client", InputDigest: hex.EncodeToString(selectionSum[:]), PromptVersion: "context-selection-v1"}))
	r, err = s.Read(ctx, p, "one", started)
	op = Operation{}
	if err != nil || decode(r.Value, &op) != nil {
		t.Fatal(err)
	}
	if _, err := s.ServerModel(ctx, p, "one", selection.ID, serverModelRequest{Client: "client", Turn: *op.Turn, Body: selectionBody}); err != nil {
		t.Fatal("server Context selection", err)
	}
	r, err = s.Read(ctx, p, "one", Ref{ID: selection.ID, Version: "head"})
	op = Operation{}
	if err != nil || decode(r.Value, &op) != nil || op.State != "applied" || op.Applied == nil {
		t.Fatal("selection not applied", op.State, op.Reason, err)
	}
	var runsAfter int
	if err := s.Store.Database().QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&runsAfter); err != nil || runsBefore != runsAfter {
		t.Fatal("selection started a Run", err)
	}
}
