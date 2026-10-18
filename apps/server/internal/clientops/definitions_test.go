package clientops

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
)

func checkDefinitions(t *testing.T, s *Service) {
	ctx := context.Background()
	p := access.Principal{UserID: "alice"}
	serial := 0
	m := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("generated-", serial), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		ref, e := accepted(v, err)
		if e != nil {
			t.Fatal("generated mutation", v.Error, err)
		}
		return ref
	}
	rs := runs.New(s.Store, s.Access, s.Contexts)
	s.Definitions = workflows.New(s.Store, s.Access, s.Contexts, rs)
	if _, err := s.Store.Database().Exec(ctx, `INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('generation-device','one','alice','generation-device','generation-device','g',true,true)`); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(strings.Repeat("a", 32)))
	mac.Write([]byte("device\x00generation-device\x00g"))
	device, err := s.Access.DevicePrincipal(ctx, "one", "d.generation-device."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	capability := must(rs.ContactRunner(ctx, device, "one", runs.RunnerContact{Mutation: m("0"), RunnerID: "owned-generation-runner", Connections: []runs.Connection{
		{Kind: "cli", Target: "codex", Version: "0.162.1", Environment: "windows", Subject: "owned-codex", Confirmed: true, Bindings: []string{"stdin"}},
		{Kind: "cli", Target: "claude", Version: "2.1.300", Environment: "windows", Subject: "owned-claude", Confirmed: true, Bindings: []string{"stdin"}},
	}}))
	readOp := func(ref Ref) Operation {
		t.Helper()
		r, err := s.Read(ctx, p, "one", ref)
		if err != nil {
			t.Fatal(err)
		}
		var op Operation
		json.Unmarshal(r.Value, &op)
		return op
	}
	policy := DefaultPolicy()
	policy.Fixed.Mode = "none"
	policy.Definition = &DefinitionPolicy{FixedPaths: []string{}, Sources: []Ref{capability}}
	policyRef := must(s.PutPolicy(ctx, p, "one", "definition-policy:generated", Write{Mutation: m("0"), Value: raw(policy)}))
	start := func(id string, target Ref, policy Ref, model string) Ref {
		t.Helper()
		return must(s.Start(ctx, p, "one", "client-operation:generated-"+id, Start{Mutation: m("0"), Policy: policy, Target: target, Goal: "Use the declared CLI and user-provided workspace; do not execute it", Purpose: "definition", Client: "generation-client", Model: Model{Connection: model, Revision: "v1", Requested: model, Allowed: []string{model}}}))
	}
	finish := func(ref Ref, proposal any) Ref {
		t.Helper()
		sending := must(s.Begin(ctx, p, "one", ref.ID, Begin{Mutation: m(ref.Version), Client: "generation-client", InputDigest: strings.Repeat("d", 64), PromptVersion: "definition-v1"}))
		op := readOp(sending)
		response := raw(map[string]any{"id": fmt.Sprint("generated-response-", serial), "model": op.Model.Requested, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(raw(proposal))}}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 10}})
		req := Finish{Mutation: m(sending.Version), Client: "generation-client", Turn: *op.Turn, Response: response}
		saved := must(s.Finish(ctx, p, "one", ref.ID, req))
		if again := must(s.Finish(ctx, p, "one", ref.ID, req)); again != saved {
			t.Fatal("duplicate definition result")
		}
		return saved
	}
	proposal := func(d workflows.Definition, questions []string) any {
		return map[string]any{"definition_utf8": string(raw(d)), "reason": "Based on the declared connection and request", "questions": questions, "unknown": []string{}}
	}
	var latest Ref
	var original workflows.Definition
	for _, cli := range []string{"codex", "claude"} {
		d := workflows.Definition{Schema: "run-definition-v1", Name: "Generated " + cli, DeviceID: "generation-device", Selection: &contexts.Selection{Mode: "none"}, Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: cli, Parameters: raw([]string{})}}}}
		created := start(cli, Ref{ID: "run-definition:generated-" + cli, Version: "0"}, policyRef, "owned-model-"+cli)
		completed := finish(created, proposal(d, []string{}))
		op := readOp(completed)
		if op.State != "applied" || op.Applied == nil {
			t.Fatal("definition was not adopted", op.State, op.Reason)
		}
		r, err := s.Definitions.Read(ctx, p, "one", *op.Applied)
		if err != nil {
			t.Fatal(err)
		}
		var adoption workflows.Adoption
		json.Unmarshal(r.Value, &adoption)
		actual, err := s.Definitions.Read(ctx, p, "one", adoption.Definition)
		if err != nil {
			t.Fatal(err)
		}
		var got workflows.Definition
		json.Unmarshal(actual.Value, &got)
		if got.Spec.Input.Command.Target != cli || got.Name != d.Name {
			t.Fatal("different definition adopted")
		}
		latest, original = adoption.Definition, d
		var count int
		if err := s.Store.Database().QueryRow(ctx, `SELECT count(DISTINCT projection_key) FROM usage_entries WHERE attribution_kind='operation' AND attribution_id=$1 AND kind='usage'`, created.ID).Scan(&count); err != nil || count != 1 {
			t.Fatal("definition usage", count, err)
		}
	}
	manualPolicy := policy
	manualPolicy.Definition = &DefinitionPolicy{Sources: []Ref{capability}, FixedPaths: []string{"/name"}, FixedValues: map[string]string{"/name": `"Manually authored unsent name"`}}
	manualPolicyRef := must(s.PutPolicy(ctx, p, "one", "definition-policy:manual-snapshot", Write{Mutation: m("0"), Value: raw(manualPolicy)}))
	manual := start("manual-snapshot", Ref{ID: "run-definition:manual-snapshot", Version: "0"}, manualPolicyRef, "owned-model-claude")
	var partial map[string]json.RawMessage
	json.Unmarshal(raw(original), &partial)
	delete(partial, "name")
	manual = finish(manual, map[string]any{"definition_utf8": string(raw(partial)), "reason": "Keep the manual fixed value without receiving it", "questions": []string{}, "unknown": []string{}})
	if readOp(manual).State != "applied" {
		t.Fatal("unsent fixed field was lost")
	}
	actual, err := s.Definitions.Read(ctx, p, "one", Ref{ID: "run-definition:manual-snapshot", Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	var preserved workflows.Definition
	json.Unmarshal(actual.Value, &preserved)
	if preserved.Name != "Manually authored unsent name" {
		t.Fatal("manual value replaced")
	}
	questionPolicy := policy
	questionPolicy.Definition = &DefinitionPolicy{Sources: []Ref{}, FixedPaths: []string{}}
	questionPolicyRef := must(s.PutPolicy(ctx, p, "one", "definition-policy:question", Write{Mutation: m("0"), Value: raw(questionPolicy)}))
	question := start("question", Ref{ID: "run-definition:generated-question", Version: "0"}, questionPolicyRef, "owned-model-codex")
	question = finish(question, proposal(workflows.Definition{Schema: "run-definition-v1", Name: "Incomplete"}, []string{"Which CLI, device and working directory?"}))
	if readOp(question).State != "question" {
		t.Fatal("missing information fabricated")
	}
	answered := must(s.Act(ctx, p, "one", question.ID, Action{Mutation: m(question.Version), Kind: "answer", Client: "generation-client", Answer: "Use the declared Claude connection and the supplied owned workspace", DefinitionSources: []Ref{capability}}))
	if readOp(finish(answered, proposal(original, []string{}))).State != "applied" {
		t.Fatal("answer did not produce an adopted version")
	}
	stale := start("stale", latest, policyRef, "owned-model-claude")
	changed := original
	changed.Name = "Manual editor's new version"
	if _, err := s.Store.Database().Exec(ctx, `UPDATE access_members SET role='editor' WHERE workspace_id='one' AND user_id='bob'`); err != nil {
		t.Fatal(err)
	}
	updated := must(s.Definitions.Put(ctx, access.Principal{UserID: "bob"}, "one", latest.ID, workflows.Write{Mutation: m(latest.Version), Value: raw(changed)}))
	if readOp(finish(stale, proposal(original, []string{}))).State != "proposal" {
		t.Fatal("stale definition overwrote another editor")
	}
	policy.Definition.FixedPaths = []string{"/name"}
	fixedPolicy := must(s.PutPolicy(ctx, p, "one", "definition-policy:fixed", Write{Mutation: m("0"), Value: raw(policy)}))
	fixed := start("fixed", updated, fixedPolicy, "owned-model-claude")
	changed.Spec.Input.Command.Parameters = raw([]string{"--help"})
	changed.Name = "Rejected replacement of the fixed name"
	fixed = finish(fixed, proposal(changed, []string{}))
	if readOp(fixed).State != "proposal" {
		t.Fatal("fixed field overwritten")
	}
	selected := must(s.Act(ctx, p, "one", fixed.ID, Action{Mutation: m(fixed.Version), Kind: "adopt", Client: "generation-client", Paths: []string{"/spec/input/command/parameters"}}))
	if readOp(selected).State != "applied" {
		t.Fatal("field adoption did not reuse validation")
	}

	requiredText := "Owned required Context"
	sentence := must(s.Contexts.Put(ctx, p, "one", contexts.Identity{Kind: "sentence", ID: "generation-required"}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &requiredText})}))
	requiredRef := contexts.Ref{Kind: "sentence", ID: "generation-required", Revision: sentence.Version}
	requiredPolicy := policy
	requiredPolicy.Definition = &DefinitionPolicy{Sources: []Ref{capability}, FixedPaths: []string{}}
	requiredPolicy.Fixed = contexts.Selection{Mode: "select", Include: []contexts.Ref{requiredRef}}
	requiredPolicyRef := must(s.PutPolicy(ctx, p, "one", "definition-policy:required", Write{Mutation: m("0"), Value: raw(requiredPolicy)}))
	requiredDefinition := original
	requiredDefinition.Selection = nil
	requiredDefinition.Spec.Input.Command.Binding = map[string]string{"context": "stdin"}
	requiredOp := start("required", Ref{ID: "run-definition:generated-required", Version: "0"}, requiredPolicyRef, "owned-model-claude")
	completedRequired := readOp(finish(requiredOp, proposal(requiredDefinition, []string{})))
	if completedRequired.State != "applied" {
		t.Fatal("valid required selection was not adopted", completedRequired.State)
	}
	{
		rec, err := s.Definitions.Read(ctx, p, "one", Ref{ID: "run-definition:generated-required", Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var got workflows.Definition
		json.Unmarshal(rec.Value, &got)
		if got.Selection == nil || len(got.Selection.Include) != 1 || got.Selection.Include[0] != requiredRef {
			t.Fatal("required Context disappeared when model omitted selection")
		}
	}

	emptyPolicy := policy
	emptyPolicy.Definition = &DefinitionPolicy{Sources: []Ref{capability}, FixedPaths: []string{"/inputs", "/required"}, FixedValues: map[string]string{"/inputs": "[]", "/required": "[]"}}
	emptyPolicyRef := must(s.PutPolicy(ctx, p, "one", "definition-policy:empty-fixed", Write{Mutation: m("0"), Value: raw(emptyPolicy)}))
	emptyOp := start("empty-fixed", Ref{ID: "run-definition:empty-fixed", Version: "0"}, emptyPolicyRef, "owned-model-claude")
	if readOp(finish(emptyOp, proposal(original, []string{}))).State != "applied" {
		t.Fatal("unchanged empty fixed inputs or capabilities rejected")
	}
	var runCount, pauses, executions int
	if err := s.Store.Database().QueryRow(ctx, `SELECT (SELECT count(*) FROM run_records),(SELECT count(*) FROM run_records WHERE kind='pause'),(SELECT count(*) FROM workflow_records WHERE kind='execution')`).Scan(&runCount, &pauses, &executions); err != nil || runCount != 0 || pauses != 0 || executions != 0 {
		t.Fatal("generation started work", runCount, pauses, executions, err)
	}
	adoption := readOp(selected).Applied
	if adoption == nil {
		t.Fatal("adoption missing")
	}
	req := workflows.CreateRun{Mutation: m("0"), Adoption: *adoption, InputsUTF8: "{}"}
	created := must(s.Definitions.CreateRun(ctx, p, "one", "run:generated-explicit", req))
	if replay := must(s.Definitions.CreateRun(ctx, p, "one", "run:generated-explicit", req)); replay != created {
		t.Fatal("duplicate explicit Run")
	}
	record, err := rs.Read(ctx, p, "one", created)
	if err != nil {
		t.Fatal(err)
	}
	var run runs.Run
	json.Unmarshal(record.Value, &run)
	if run.Assignment != nil || run.ExecutionRequestedAt != nil {
		t.Fatal("Run was implicitly assigned or started")
	}
	if err := s.Store.Database().QueryRow(ctx, `SELECT count(DISTINCT id) FROM run_records WHERE kind='run'`).Scan(&runCount); err != nil || runCount != 1 {
		t.Fatal("normal Run creation", runCount, err)
	}
	t.Log("two model connections, declared CLI capability contracts, questions, operation usage, receipts, stale targets, fixed fields and selected adoption share normal definitions without execution")
}
