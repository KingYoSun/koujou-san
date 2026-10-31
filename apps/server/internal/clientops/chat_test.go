package clientops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

func checkProjectChat(t *testing.T, s *Service, url string) {
	ctx := context.Background()
	p := access.Principal{UserID: "alice"}
	bob := access.Principal{UserID: "bob"}
	if _, err := s.Store.Database().Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='one' AND user_id='bob'`); err != nil {
		t.Fatal(err)
	}
	serial := 0
	m := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("chat-test-%d", serial), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		ref, e := accepted(v, err)
		if e != nil {
			t.Fatalf("chat mutation: %+v %v", v.Error, err)
		}
		return ref
	}
	read := func(ref Ref, target any) Record {
		t.Helper()
		r, err := s.Read(ctx, p, "one", ref)
		if err != nil || json.Unmarshal(r.Value, target) != nil {
			t.Fatalf("chat read: %v %s", err, r.Value)
		}
		return r
	}
	project := must(s.Contexts.Put(ctx, p, "one", contexts.Identity{Kind: "scope", ID: "chat-project"}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Scope{Kind: "project", Name: "Chat project"})}))
	threadRequest := Write{Mutation: m("0"), Value: raw(ChatThread{Name: "Issueなしの相談", Project: project})}
	thread := must(s.PutChatThread(ctx, p, "one", "chat-thread:one", threadRequest))
	if must(s.PutChatThread(ctx, p, "one", thread.ID, threadRequest)) != thread {
		t.Fatal("lost thread ACK created another version")
	}
	humanRequest := Write{Mutation: m("0"), Value: raw(ChatMessage{Thread: thread, Role: "human", Text: "相談です。採用する仕様。余談です。", State: "complete"})}
	human := must(s.PutChatMessage(ctx, p, "one", "chat-message:human", humanRequest))
	if must(s.PutChatMessage(ctx, p, "one", human.ID, humanRequest)) != human {
		t.Fatal("lost message ACK duplicated history")
	}
	if _, err := s.PutChatMessage(ctx, bob, "one", "chat-message:viewer", humanRequest); err == nil {
		t.Fatal("viewer saved a message")
	}
	if _, err := s.Read(ctx, access.Principal{UserID: "other"}, "one", human); err == nil {
		t.Fatal("another Workspace read chat")
	}
	if _, err := s.PutChatMessage(ctx, p, "one", "chat-message:fake-ai", Write{Mutation: m("0"), Value: raw(ChatMessage{Thread: thread, Role: "assistant", Text: "PASS", State: "complete"})}); err == nil {
		t.Fatal("human request impersonated model results")
	}
	policy := DefaultPolicy()
	policy.Mode, policy.Fixed.Mode = "proposal", "none"
	policy.Chat = &ChatSource{Thread: thread, Messages: []Ref{human}}
	policyRef := must(s.PutPolicy(ctx, p, "one", "chat-policy:one", Write{Mutation: m("0"), Value: raw(policy)}))
	model := Model{Connection: "owned-chat", Revision: "v1", Requested: "owned", Allowed: []string{"owned"}}
	start := func(id string, policy Ref) Ref {
		t.Helper()
		return must(s.Start(ctx, p, "one", "client-operation:chat-"+id, Start{Mutation: m("0"), Policy: policy, Target: thread, Goal: "Projectの相談", Purpose: "chat", Model: model, Client: "client"}))
	}
	operation := start("one", policyRef)
	var op Operation
	read(operation, &op)
	var runsBefore int
	_ = s.Store.Database().QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&runsBefore)
	beginRequest := Begin{Mutation: m(operation.Version), Client: "client", InputDigest: strings.Repeat("a", 64), PromptVersion: "project-chat-v1"}
	sending := must(s.Begin(ctx, p, "one", operation.ID, beginRequest))
	if must(s.Begin(ctx, p, "one", operation.ID, beginRequest)) != sending {
		t.Fatal("lost intent ACK reserved another model call")
	}
	read(sending, &op)
	if op.State != "sending" {
		t.Fatal(op.State)
	}
	reply := raw(map[string]any{"id": "owned-chat-response", "model": "owned", "choices": []any{map[string]any{"message": map[string]any{"content": "採用する仕様をContextへ保存できます。"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 9, "completion_tokens": 5}})
	finishRequest := Finish{Mutation: m(sending.Version), Client: "client", Turn: *op.Turn, Response: reply, Status: 200}
	done := must(s.Finish(ctx, p, "one", sending.ID, finishRequest))
	if must(s.Finish(ctx, p, "one", sending.ID, finishRequest)) != done {
		t.Fatal("lost result ACK duplicated reply/usage")
	}
	read(done, &op)
	if op.State != "completed" || op.Applied == nil {
		t.Fatal(op.State, op.Reason)
	}
	var assistant ChatMessage
	read(*op.Applied, &assistant)
	if assistant.Role != "assistant" || assistant.State != "complete" || assistant.Operation == nil {
		t.Fatal(assistant)
	}
	t.Run("read_tools_and_incomplete_responses_keep_their_states", func(t *testing.T) {
		operation := start("tools", policyRef)
		sending := must(s.Begin(ctx, p, "one", operation.ID, Begin{Mutation: m(operation.Version), Client: "client", InputDigest: strings.Repeat("b", 64), PromptVersion: "project-chat-v1"}))
		var op Operation
		read(sending, &op)
		toolReply := raw(map[string]any{"id": "chat-tool", "model": "owned", "choices": []any{map[string]any{"message": map[string]any{"content": nil, "tool_calls": []any{map[string]any{"id": "search", "type": "function", "function": map[string]string{"name": "context_search", "arguments": `{}`}}}}, "finish_reason": "tool_calls"}}})
		ready := must(s.Finish(ctx, p, "one", sending.ID, Finish{Mutation: m(sending.Version), Client: "client", Turn: *op.Turn, Response: toolReply}))
		read(ready, &op)
		if op.State != "ready" || op.Applied != nil {
			t.Fatal("read-only model tool could not continue", op)
		}
		sending = must(s.Begin(ctx, p, "one", ready.ID, Begin{Mutation: m(ready.Version), Client: "client", InputDigest: strings.Repeat("c", 64), PromptVersion: "project-chat-v1"}))
		read(sending, &op)
		partial := raw(map[string]any{"id": "chat-partial", "model": "owned", "choices": []any{map[string]any{"message": map[string]string{"content": "途中の応答"}, "finish_reason": "length"}}})
		incomplete := must(s.Finish(ctx, p, "one", sending.ID, Finish{Mutation: m(sending.Version), Client: "client", Turn: *op.Turn, Response: partial}))
		read(incomplete, &op)
		if op.State != "incomplete" || op.Applied == nil {
			t.Fatal("partial response reported complete", op)
		}
		if _, err := s.AdoptChat(ctx, p, "one", "chat-adoption:incomplete", ChatAdopt{Mutation: m("0"), Message: *op.Applied, End: len("途中の応答")}); err == nil {
			t.Fatal("incomplete reply adopted as specification")
		}
	})
	t.Run("current_delivery_policy_applies_before_reserving_a_request", func(t *testing.T) {
		operation := start("denied-scope", policyRef)
		previous, version, err := s.Access.DeliveryPolicy(ctx, p, "one")
		if err != nil {
			t.Fatal(err)
		}
		policy := previous
		policy.DeniedScopes = append(append([]string{}, previous.DeniedScopes...), "chat-project")
		written := must(s.Access.SetDeliveryPolicy(ctx, p, "one", m(version), policy))
		if _, err := s.Begin(ctx, p, "one", operation.ID, Begin{Mutation: m(operation.Version), Client: "client", InputDigest: strings.Repeat("d", 64), PromptVersion: "project-chat-v1"}); err == nil {
			t.Fatal("model request reserved after Project delivery was prohibited")
		}
		must(s.Access.SetDeliveryPolicy(ctx, p, "one", m(written.Version), previous))
		var count int
		if err := s.Store.Database().QueryRow(ctx, `SELECT models FROM client_operation_heads WHERE workspace_id='one' AND id=$1`, operation.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal(count, err)
		}
	})
	t.Run("Project_read_permission_does_not_grant_model_writes", func(t *testing.T) {
		exec := func(sql string, args ...any) {
			t.Helper()
			if _, err := s.Store.Database().Exec(ctx, sql, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`UPDATE access_members SET role='editor' WHERE workspace_id='one' AND user_id='bob'`)
		exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, project.ID)
		exec(`INSERT INTO access_resource_grants(workspace_id,resource_id,user_id,can_read,can_update,can_execute) VALUES('one',$1,'alice',true,true,false),('one',$1,'bob',true,false,false)`, project.ID)
		t.Cleanup(func() {
			exec(`UPDATE access_members SET role='viewer' WHERE workspace_id='one' AND user_id='bob'`)
			exec(`UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id=$1`, project.ID)
			exec(`DELETE FROM access_resource_grants WHERE workspace_id='one' AND resource_id=$1 AND user_id IN ('alice','bob')`, project.ID)
		})
		if _, err := s.Read(ctx, bob, "one", human); err != nil {
			t.Fatal("reader cannot read conversation", err)
		}
		bobPolicy := must(s.PutPolicy(ctx, bob, "one", "chat-policy:reader", Write{Mutation: m("0"), Value: raw(policy)}))
		request := Start{Mutation: m("0"), Policy: bobPolicy, Target: thread, Goal: "Projectの相談", Purpose: "chat", Model: model, Client: "bob-client"}
		if out, err := s.Start(ctx, bob, "one", "client-operation:chat-read-denied", request); err == nil && out.Error == nil {
			t.Fatal("Project reader started a model write")
		}
		write := func(allowed bool) {
			exec(`UPDATE access_resource_grants SET can_update=$2 WHERE workspace_id='one' AND resource_id=$1 AND user_id='bob'`, project.ID, allowed)
		}
		write(true)
		request.Mutation = m("0")
		operation := must(s.Start(ctx, bob, "one", "client-operation:chat-revoked", request))
		write(false)
		begin := Begin{Mutation: m(operation.Version), Client: "bob-client", InputDigest: strings.Repeat("e", 64), PromptVersion: "project-chat-v1"}
		if out, err := s.Begin(ctx, bob, "one", operation.ID, begin); err == nil && out.Error == nil {
			t.Fatal("revoked writer reserved a model request")
		}
		write(true)
		begin.Mutation = m(operation.Version)
		sending := must(s.Begin(ctx, bob, "one", operation.ID, begin))
		var op Operation
		r, err := s.Read(ctx, bob, "one", sending)
		if err != nil || json.Unmarshal(r.Value, &op) != nil {
			t.Fatal(err)
		}
		write(false)
		if out, err := s.Finish(ctx, bob, "one", sending.ID, Finish{Mutation: m(sending.Version), Client: "bob-client", Turn: *op.Turn, Response: reply}); err == nil && out.Error == nil {
			t.Fatal("revoked writer saved an assistant message")
		}
	})
	page, err := s.Query(ctx, bob, "one", Request{Root: thread.ID, Kind: "chat-message", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Ref != human {
		t.Fatal(page, err)
	}
	next, err := s.Query(ctx, bob, "one", Request{Root: thread.ID, Kind: "chat-message", Limit: 1, After: page.After, AfterID: page.AfterID})
	if err != nil || len(next.Items) != 1 || next.Items[0].Ref != *op.Applied {
		t.Fatal(next, err)
	}
	adoptionRequest := ChatAdopt{Mutation: m("0"), Message: human, Start: len("相談です。"), End: len("相談です。採用する仕様。"), Name: "採用仕様"}
	adopted := must(s.AdoptChat(ctx, p, "one", "chat-adoption:one", adoptionRequest))
	if must(s.AdoptChat(ctx, p, "one", adopted.ID, adoptionRequest)) != adopted {
		t.Fatal("adoption replay created a second Context")
	}
	var adoption ChatAdoption
	read(adopted, &adoption)
	contextRecord, err := s.Contexts.Read(ctx, p, "one", adoption.Context)
	var unit contexts.Unit
	_ = json.Unmarshal(contextRecord.Value, &unit)
	if err != nil || unit.Text == nil || *unit.Text != "採用する仕様。" || unit.Origin == nil || unit.Origin.ID != human.ID || unit.Origin.Revision != human.Version || unit.Origin.Path != fmt.Sprintf("/text/utf8/%d:%d", adoptionRequest.Start, adoptionRequest.End) {
		t.Fatal(unit, err)
	}
	var runsAfter, usageCount, models int
	err = s.Store.Database().QueryRow(ctx, `SELECT (SELECT count(*) FROM run_records),(SELECT count(DISTINCT projection_key) FROM usage_entries WHERE attribution_kind='operation' AND attribution_id=$1 AND kind='usage'),(SELECT models FROM client_operation_heads WHERE workspace_id='one' AND id=$1)`, operation.ID).Scan(&runsAfter, &usageCount, &models)
	if err != nil || runsBefore != runsAfter || usageCount != 1 || models != 1 {
		t.Fatal("conversation/adoption started a Run or duplicated usage", runsBefore, runsAfter, usageCount, models, err)
	}
	reopened, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered := &Service{Store: reopened, Access: s.Access}
	if r, err := recovered.Read(ctx, p, "one", human); err != nil || r.Ref != human {
		t.Fatal("API restart lost history", r, err)
	}
	if _, err := s.Store.Database().Exec(ctx, `UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1;`, project.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.Store.Database().Exec(ctx, `UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id=$1`, project.ID)
	})
	if _, err := s.Read(ctx, bob, "one", human); err == nil {
		t.Fatal("revoked Project access still read messages")
	}
	denied, err := s.Query(ctx, bob, "one", Request{Root: thread.ID, Kind: "chat-message", Limit: 50})
	if err != nil || len(denied.Items) != 0 {
		t.Fatal("message listing leaked a revoked Project", denied, err)
	}
}
