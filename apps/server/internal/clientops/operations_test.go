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
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

func checkOperations(t *testing.T, s *Service) {
	ctx := context.Background()
	p := access.Principal{UserID: "alice"}
	serial := 0
	m := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("flow-", serial), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		r, e := accepted(v, err)
		if e != nil {
			t.Fatalf("mutation: %+v / %v / %v", v.Error, err, e)
		}
		return r
	}
	read := func(ref Ref) Operation {
		t.Helper()
		r, err := s.Read(ctx, p, "one", ref)
		if err != nil {
			t.Fatal(err)
		}
		var op Operation
		if err = json.Unmarshal(r.Value, &op); err != nil {
			t.Fatal(err)
		}
		return op
	}
	policy := DefaultPolicy()
	policy.Fixed.Mode = "none"
	policyRef := must(s.PutPolicy(ctx, p, "one", "selection-policy:flow", Write{Mutation: m("0"), Value: raw(policy)}))
	model := Model{Connection: "owned", Revision: "v1", Requested: "owned-model", Allowed: []string{"owned-model"}}
	t.Run("definition_generation_can_start_before_a_Run_exists", func(t *testing.T) {
		var value map[string]any
		_ = json.Unmarshal(raw(policy), &value)
		value["definition"] = map[string]any{"fixed_paths": []string{}, "sources": []Ref{}}
		ref := must(s.PutPolicy(ctx, p, "one", "definition-policy:flow", Write{Mutation: m("0"), Value: raw(value)}))
		created := must(s.Start(ctx, p, "one", "client-operation:definition-before-run", Start{Mutation: m("0"), Policy: ref, Target: Ref{ID: "run-definition:generated", Version: "0"}, Goal: "create a definition without starting a command", Purpose: "definition", Model: model, Client: "alpha"}))
		if read(created).State != "ready" {
			t.Fatal("definition operation is not ready")
		}
	})
	start := func(label string, policy Ref) Ref {
		t.Helper()
		return must(s.Start(ctx, p, "one", "client-operation:"+label, Start{Mutation: m("0"), Policy: policy, Target: Ref{ID: "client-selection:" + label, Version: "0"}, Goal: "select existing Context according to the fixed policy", Purpose: "selection", Model: model, Client: "alpha"}))
	}
	begin := func(ref Ref, client string) Ref {
		t.Helper()
		return must(s.Begin(ctx, p, "one", ref.ID, Begin{Mutation: m(ref.Version), Client: client, InputDigest: strings.Repeat("a", 64), PromptVersion: "fixture-v1"}))
	}
	t.Run("forbidden_required_reference_is_not_reserved_for_model_delivery", func(t *testing.T) {
		text := "ordinary manual-only Context"
		saved := must(s.Contexts.Put(ctx, p, "one", contexts.Identity{Kind: "sentence", ID: "required-no-export"}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text, Tags: []string{"no-export"}})}))
		input := contexts.Selection{Mode: "select", Include: []contexts.Ref{{Kind: "sentence", ID: "required-no-export", Revision: saved.Version}}}
		must(s.PutSelection(ctx, p, "one", "client-selection:manual-no-export", SelectionWrite{Mutation: m("0"), Input: input}))
		policy := DefaultPolicy()
		policy.Fixed = input
		policyRef := must(s.PutPolicy(ctx, p, "one", "selection-policy:required-no-export", Write{Mutation: m("0"), Value: raw(policy)}))
		created := start("required-no-export", policyRef)
		if _, err := s.Begin(ctx, p, "one", created.ID, Begin{Mutation: m(created.Version), Client: "alpha", InputDigest: strings.Repeat("a", 64), PromptVersion: "fixture-v1"}); err == nil {
			t.Fatal("forbidden required ID/version can enter the initial model request")
		}
		var count int
		if err := s.Store.Database().QueryRow(ctx, `SELECT models FROM client_operation_heads WHERE workspace_id='one' AND id=$1`, created.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("forbidden initial input reserved a model request", count, err)
		}
	})
	reply := func(proposal Proposal) json.RawMessage {
		return raw(map[string]any{"id": fmt.Sprint("owned-", serial), "model": "owned-model", "choices": []any{map[string]any{"message": map[string]any{"content": string(raw(proposal))}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4, "prompt_tokens_details": map[string]any{"cached_tokens": 0}, "completion_tokens_details": map[string]any{"reasoning_tokens": 0}}})
	}
	finish := func(ref Ref, client string, b json.RawMessage) Ref {
		t.Helper()
		op := read(ref)
		return must(s.Finish(ctx, p, "one", ref.ID, Finish{Mutation: m(ref.Version), Client: client, Turn: *op.Turn, Response: b}))
	}
	empty := Proposal{Items: []Item{}, Questions: []string{}, Unknown: []string{}}
	// A lost ACK uses the same intent/response and creates one selection/usage.
	created := start("auto-flow", policyRef)
	sending := begin(created, "alpha")
	op := read(sending)
	if op.State != "sending" || op.Turn == nil {
		t.Fatal(op)
	}
	req := Finish{Mutation: m(sending.Version), Client: "alpha", Turn: *op.Turn, Response: reply(empty)}
	finished := must(s.Finish(ctx, p, "one", sending.ID, req))
	replayed := must(s.Finish(ctx, p, "one", sending.ID, req))
	if finished != replayed {
		t.Fatal("duplicate response version")
	}
	op = read(finished)
	if op.State != "applied" || op.Applied == nil {
		t.Fatal("auto selection not adopted", op.State, op.Reason)
	}
	var runCount, usageCount int
	if err := s.Store.Database().QueryRow(ctx, `SELECT (SELECT count(*) FROM run_records),(SELECT count(DISTINCT projection_key) FROM usage_entries WHERE attribution_kind='operation' AND attribution_id=$1 AND kind='usage')`, created.ID).Scan(&runCount, &usageCount); err != nil || runCount != 0 || usageCount != 1 {
		t.Fatal(runCount, usageCount, err)
	}
	// Even an editor cannot answer or take over another requester's operation.
	if _, err := s.Store.Database().Exec(ctx, `UPDATE access_members SET role='editor' WHERE workspace_id='one' AND user_id='bob'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.Store.Database().Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='one' AND user_id='bob'`)
	})
	created = start("handover-flow", policyRef)
	sending = begin(created, "alpha")
	if _, err := s.Act(ctx, access.Principal{UserID: "bob"}, "one", created.ID, Action{Mutation: m(sending.Version), Client: "beta", Kind: "takeover"}); err == nil {
		t.Fatal("another member took over")
	}
	transferred := must(s.Act(ctx, p, "one", created.ID, Action{Mutation: m(sending.Version), Client: "beta", Kind: "takeover"}))
	late := finish(transferred, "alpha", reply(empty))
	op = read(late)
	if op.State != "proposal" || op.Reason != "client_changed" || op.Applied != nil {
		t.Fatal("late reply auto-applied", op)
	}
	if _, err := s.Begin(ctx, p, "one", late.ID, Begin{Mutation: m(late.Version), Client: "beta", InputDigest: strings.Repeat("b", 64), PromptVersion: "v1"}); err == nil {
		t.Fatal("reconnect generated again")
	}
	adopted := must(s.Act(ctx, p, "one", created.ID, Action{Mutation: m(late.Version), Client: "beta", Kind: "adopt"}))
	if read(adopted).State != "applied" {
		t.Fatal("explicit adoption failed")
	}
	// Questions belong to the client operation, and can resume after explicit transfer.
	created = start("question-flow", policyRef)
	sending = begin(created, "alpha")
	question := empty
	question.Questions = []string{"対象は？"}
	question.Unknown = []string{"対象の指定待ち"}
	asked := finish(sending, "alpha", reply(question))
	if read(asked).State != "question" {
		t.Fatal("question not resumable")
	}
	transferred = must(s.Act(ctx, p, "one", created.ID, Action{Mutation: m(asked.Version), Client: "beta", Kind: "takeover"}))
	answered := must(s.Act(ctx, p, "one", created.ID, Action{Mutation: m(transferred.Version), Client: "beta", Kind: "answer", Answer: "none"}))
	sending = begin(answered, "beta")
	if read(finish(sending, "beta", reply(empty))).State != "applied" {
		t.Fatal("answer did not resume")
	}
	// A completed result after the time budget remains an inspectable draft.
	created = start("expired-flow", policyRef)
	sending = begin(created, "alpha")
	if _, err := s.Store.Database().Exec(ctx, `UPDATE client_operation_heads SET deadline=now()-interval '1 second' WHERE workspace_id='one' AND id=$1`, created.ID); err != nil {
		t.Fatal(err)
	}
	expired := finish(sending, "alpha", reply(empty))
	if op = read(expired); op.State != "incomplete" || op.Applied != nil {
		t.Fatal("expired result applied", op)
	}
	policy.Limits.Cost = &usage.CostBudget{Amount: "0", Currency: "USD", Valuation: "estimated"}
	zero := must(s.PutPolicy(ctx, p, "one", "selection-policy:zero-cost", Write{Mutation: m("0"), Value: raw(policy)}))
	created = start("zero-cost-flow", zero)
	blocked := begin(created, "alpha")
	if op = read(blocked); op.State != "incomplete" || op.Turn != nil {
		t.Fatal("zero budget sent a model request", op)
	}
	policy.Limits.Cost = nil
	policy.Limits.Models = 1
	one := must(s.PutPolicy(ctx, p, "one", "selection-policy:one-call", Write{Mutation: m("0"), Value: raw(policy)}))
	created = start("one-call-flow", one)
	sending = begin(created, "alpha")
	tool := raw(map[string]any{"id": "owned-tool-response", "model": "owned-model", "choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "tool-1", "type": "function", "function": map[string]any{"name": "context_search", "arguments": "{\"text\":\"owned\",\"limit\":10}"}}}}, "finish_reason": "tool_calls"}}})
	ready := finish(sending, "alpha", tool)
	blocked = begin(ready, "alpha")
	if read(blocked).State != "incomplete" {
		t.Fatal("model request limit ignored")
	}
	page, err := s.Query(ctx, p, "one", Request{Root: "client-operation:question-flow", History: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	foundAnswer := false
	for _, r := range page.Items {
		if r.Root != "client-operation:question-flow" {
			t.Fatal("other operation history")
		}
		foundAnswer = foundAnswer || r.Kind == "answer"
	}
	if !foundAnswer {
		t.Fatal("answer history missing")
	}
	// A complete candidate page can be sufficient without visiting every match.
	text := "owned candidate"
	ref := must(s.Contexts.Put(ctx, p, "one", contexts.Identity{Kind: "sentence", ID: "page-choice"}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
	chosen := contexts.Ref{Kind: "sentence", ID: "page-choice", Revision: ref.Version}
	pagePolicy := DefaultPolicy()
	pagePolicyRef := must(s.PutPolicy(ctx, p, "one", "selection-policy:page-choice", Write{Mutation: m("0"), Value: raw(pagePolicy)}))
	created = start("page-choice", pagePolicyRef)
	value := read(created)
	next := "opaque-next-page"
	if err := s.Access.RecordOperation(ctx, access.Principal{UserID: "alice", OperationID: value.ReadOperation}, "one", "context_search", "owned", access.OperationEvent{Refs: []access.VersionRef{{ID: "context:page-choice", Version: ref.Version}}, QueryKey: "owned-query", Outcome: "returned", Range: "candidate_page", Meta: apicontract.Meta{NextCursor: &next, Completeness: apicontract.Completeness{State: "complete"}}}); err != nil {
		t.Fatal(err)
	}
	sending = begin(created, "alpha")
	choice := empty
	choice.Items = []Item{{Ref: chosen, Reason: "取得した候補ページの条件に合う"}}
	completed := finish(sending, "alpha", reply(choice))
	if op = read(completed); op.State != "applied" {
		t.Fatal("ordinary pagination forced a full scan", op.State, op.Reason)
	}
	created = start("projection-pending", pagePolicyRef)
	value = read(created)
	if err := s.Access.RecordOperation(ctx, access.Principal{UserID: "alice", OperationID: value.ReadOperation}, "one", "context_search", "owned", access.OperationEvent{Refs: []access.VersionRef{{ID: "context:page-choice", Version: ref.Version}}, QueryKey: "pending-query", Outcome: "returned", Meta: apicontract.Meta{Completeness: apicontract.Completeness{State: "pending", Reason: "projection_lag"}}}); err != nil {
		t.Fatal(err)
	}
	sending = begin(created, "alpha")
	pending := finish(sending, "alpha", reply(choice))
	if op = read(pending); op.State != "proposal" || op.Applied != nil {
		t.Fatal("incomplete projection adopted", op)
	}
	// One valued request must not hide another request's missing valuation.
	for _, r := range []struct {
		key, group, kind, state string
		amount                  *string
	}{{"known:usage:", "known", "usage", "confirmed", nil}, {"known:reported:USD", "known", "reported", "rated", new(string)}, {"missing:usage:", "missing", "usage", "unknown", nil}} {
		if r.amount != nil {
			*r.amount = "1.00"
		}
		if _, err := s.Store.Database().Exec(ctx, `INSERT INTO usage_entries VALUES('one',$1,99999,$2,$3,'USD',now(),'operation','budget-fixture','',ARRAY[]::text[],'owned-model',$4,$5,'[]','{}','{}')`, r.key, r.group, r.kind, r.amount, r.state); err != nil {
			t.Fatal(err)
		}
	}
	cost, err := usage.ObserveCosts(ctx, s.Store.Database(), p, "one", "operation", []string{"budget-fixture"}, usage.CostBudget{Amount: "10", Currency: "USD", Valuation: "reported"})
	if err != nil || cost.State != "unobserved" || cost.Amount != nil {
		t.Fatal("missing request cost became zero", cost, err)
	}
}
