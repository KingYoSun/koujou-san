package workflows

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func executionScenario(t *testing.T, s *Service, device access.Principal, mutation func(string) apicontract.Mutation) {
	ctx := context.Background()
	admin := access.Principal{UserID: "admin"}
	editor := access.Principal{UserID: "editor"}
	w := "one"
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		ref, err := accepted(v, err)
		if err != nil {
			t.Fatal(v, err)
		}
		return ref
	}
	put := func(id string, d Definition) Ref {
		return must(s.Put(ctx, editor, w, id, Write{Mutation: mutation("0"), Value: raw(d)}))
	}
	adopt := func(def Ref) Ref {
		v := must(s.Verify(ctx, editor, w, Verify{Mutation: mutation("0"), Definition: def}))
		return must(s.Adopt(ctx, editor, w, Adopt{Mutation: mutation("0"), Definition: def, Validation: v}))
	}
	plain := plainDefinition()
	plain.DeviceID = "worker"
	plain.Spec.Input.Command.Parameters = raw(map[string]any{"arg": "initial"})
	start := put("run-definition:start", plain)
	judgeDef := plain
	jSpec := *plain.Spec
	judgeDef.Spec = &jSpec
	judgeDef.Spec.Derivation = &runs.Contract{Name: "judge", Version: "one", VerdictField: "decision"}
	judgeDef.Spec.Input.Command.Binding = map[string]string{"prompt": "stdin"}
	judgeDef.Inputs = []string{"prompt"}
	judge := put("run-definition:judge", judgeDef)
	finalDef := plain
	fSpec := *plain.Spec
	finalDef.Spec = &fSpec
	finalDef.Spec.Input.Command.Binding = map[string]string{"context": "stdin", "prompt": "stdin"}
	finalDef.Inputs = []string{"context", "prompt", "command"}
	final := put("run-definition:final", finalDef)
	graph := Definition{Schema: "workflow-definition-v1", Name: "bounded scenario", Budget: &Budget{Parallel: 2, MaxRuns: 8}, Nodes: []Node{
		{ID: "a", Definition: &start}, {ID: "b", Definition: &start},
		{ID: "judge", Definition: &judge, Join: "all", Depends: []Dependency{{Node: "a", On: []string{"succeeded"}, Required: true}, {Node: "b", On: []string{"succeeded"}, Required: true}}, Inputs: []string{"prompt"}, Retry: map[string]string{"FAIL": "a"}},
		{ID: "human", Choices: []string{"approve", "rework"}, Join: "all", Depends: []Dependency{{Node: "judge", On: []string{"INCONCLUSIVE"}, Required: true}}, Retry: map[string]string{"rework": "a"}},
		{ID: "final", Definition: &final, Join: "any", Depends: []Dependency{{Node: "judge", On: []string{"PASS"}}, {Node: "human", On: []string{"approve"}}}, Bindings: []Binding{
			{From: "judge", Kind: "result", Path: []string{"selected", "context"}, To: "context"},
			{From: "judge", Kind: "result", Path: []string{"selected", "prompt"}, To: "prompt"},
			{From: "judge", Kind: "result", Path: []string{"selected", "command"}, To: "command", Parameter: []string{"arg"}},
		}},
	}}
	g := put("workflow-definition:execution", graph)
	ad := adopt(g)
	input := map[string]map[string]json.RawMessage{"judge": {"prompt": raw("inspect selected change")}}
	root := must(s.Start(ctx, admin, w, "workflow-execution:fixed", Start{Mutation: mutation("0"), Adoption: ad, Inputs: input}))
	step := func(count int) {
		t.Helper()
		for i := 0; i < count; i++ {
			if err := s.Step(ctx, w, root.ID); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
		}
	}
	state := func() map[string]Task {
		v, err := tasks(ctx, s.Store.Database(), w, root.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	report := func(node, outcome string) {
		t.Helper()
		task := state()[node]
		if task.Run == nil {
			t.Fatal("run absent", node, task)
		}
		rr, err := s.Runs.Read(ctx, admin, w, Ref{ID: task.Run.ID, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var run runs.Run
		_ = json.Unmarshal(rr.Value, &run)
		if run.Assignment == nil {
			t.Fatal("unassigned", node, run)
		}
		version := rr.Version
		for i, event := range []runs.Event{{Type: "prepared"}, {Type: "started"}, {Type: "output", Channel: "stdout", Source: "fixture", Value: raw(map[string]string{"unselected": "NEVER_INJECT"})}, {Type: "finished", ExitCode: ptr(0), Report: raw(map[string]any{"decision": outcome, "selected": map[string]any{"context": "only context", "prompt": "only prompt", "command": "only command"}, "unselected": "NEVER_INJECT", "suggestion": "start undefined command"})}} {
			event.Mutation = mutation(version)
			event.Sequence = string(rune('1' + i))
			event.Generation = run.Assignment.Generation
			if event.Type == "prepared" {
				event.PreparedArea = run.Assignment.Area
			}
			version = must(s.Runs.Report(ctx, device, w, rr.ID, event)).Version
		}
	}
	step(4)
	if state()["a"].State != "running" || state()["b"].State != "running" || state()["judge"].Run != nil {
		t.Fatal("parallel or required fan-in failed", state())
	}
	firstA := *state()["a"].Run
	report("a", "")
	step(1)
	if state()["judge"].Run != nil {
		t.Fatal("partial fan-in started")
	}
	report("b", "")
	step(3)
	report("judge", "FAIL")
	step(4)
	if state()["a"].Run == nil || state()["a"].Run.ID == firstA.ID || state()["a"].Attempt != 2 {
		t.Fatal("retry did not create new linked Run", state())
	}
	report("a", "")
	step(3)
	report("judge", "INCONCLUSIVE")
	step(3)
	human := state()["human"]
	if human.State != "decision" || state()["final"].Run != nil {
		t.Fatal("human gate not waiting", state())
	}
	e, r, err := execution(ctx, s.Store.Database(), w, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Definition != g {
		t.Fatal("definition drift")
	}
	// Answering is an edit operation; it must not grant the editor execution rights.
	denied, err := s.Decide(ctx, access.Principal{UserID: "viewer"}, w, root.ID, Decision{Mutation: mutation(r.Version), Node: "human", Attempt: human.Attempt, Choice: "rework"})
	if err == nil && denied.Error == nil {
		t.Fatal("viewer answered")
	}
	must(s.Decide(ctx, editor, w, root.ID, Decision{Mutation: mutation(r.Version), Node: "human", Attempt: human.Attempt, Choice: "rework"}))
	_, err = s.Store.Database().Exec(ctx, `UPDATE access_members SET can_execute=false WHERE workspace_id='one' AND user_id='admin'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Step(ctx, w, root.ID); err == nil {
		t.Fatal("revoked controller progressed")
	}
	_, _ = s.Store.Database().Exec(ctx, `UPDATE access_members SET can_execute=true WHERE workspace_id='one' AND user_id='admin'`)
	step(3)
	report("a", "")
	step(3)
	report("judge", "PASS")
	step(1)
	_, head, err := execution(ctx, s.Store.Database(), w, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(ctx, admin, w, Preview{Execution: head.Ref, Node: "final"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw(preview)), "NEVER_INJECT") || string(preview.Spec.Input.Context) != `"only context"` || string(preview.Spec.Input.Prompt) != `"only prompt"` {
		t.Fatal("unselected output leaked", string(raw(preview)))
	}
	step(3)
	last := state()["final"]
	if last.Prepared == nil || string(raw(last.Prepared.Spec.Input)) != string(raw(preview.Spec.Input)) {
		t.Fatal("preview/fixed input differ")
	}
	report("final", "")
	step(3)
	end, _, err := execution(ctx, s.Store.Database(), w, root.ID)
	if err != nil || end.State != "completed" || end.Count != 8 {
		t.Fatal("workflow did not finish within budget", end, err)
	}
	t.Log("parallel, required fan-in, FAIL/retry/new Run, INCONCLUSIVE/editor decision, revoked controller, PASS branch and selected input preview passed")
	// A completed fixed case is reconstructed from the stream, including original
	// outputs that were deliberately not injected into a later Run.
	_, exportRoot, _ := execution(ctx, s.Store.Database(), w, root.ID)
	httpRequest := httptest.NewRequest("GET", "/", nil)
	httpRequest.Header.Set("X-Fixture-User", "admin")
	httpRequest.SetPathValue("workspace", w)
	httpRequest.SetPathValue("id", root.ID)
	q := httpRequest.URL.Query()
	q.Set("version", exportRoot.Version)
	httpRequest.URL.RawQuery = q.Encode()
	response := httptest.NewRecorder()
	s.exportHTTP(response, httpRequest)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"type":"complete"`) || !strings.Contains(response.Body.String(), `"type":"run-record"`) || !strings.Contains(response.Body.String(), "rework") || !strings.Contains(response.Body.String(), "NEVER_INJECT") {
		t.Fatal("fixed execution export incomplete", response.Code, response.Body.String())
	}
	// Change the adopted Context and definition only through explicit application.
	text := "old Context"
	contextVersion := must(s.Contexts.Put(ctx, editor, w, contexts.Identity{Kind: "sentence", ID: "workflow-context"}, contexts.Write{Mutation: mutation("0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
	contextRef := contexts.Ref{Kind: "sentence", ID: "workflow-context", Revision: contextVersion.Version}
	baselineVersion := must(s.Contexts.Put(ctx, editor, w, contexts.Identity{Kind: "baseline", ID: "workflow-baseline"}, contexts.Write{Mutation: mutation("0"), Value: raw(contexts.Baseline{Contexts: []contexts.Ref{contextRef}})}))
	baseline := contexts.Ref{Kind: "baseline", ID: "workflow-baseline", Revision: baselineVersion.Version}
	selectedDef := plain
	selectedSpec := *plain.Spec
	selectedDef.Spec = &selectedSpec
	selectedDef.Spec.Input.Command.Binding = map[string]string{"context": "stdin"}
	selectedDef.Selection = &contexts.Selection{Mode: "select", Include: []contexts.Ref{contextRef}}
	selected := put("run-definition:selected-v1", selectedDef)
	applyGraph := Definition{Schema: "workflow-definition-v1", Name: "apply fixed version", Budget: &Budget{Parallel: 1, MaxRuns: 2}, Baseline: &baseline, Nodes: []Node{{ID: "first", Definition: &start}, {ID: "human", Choices: []string{"approve"}, Join: "all", Depends: []Dependency{{Node: "first", On: []string{"succeeded"}, Required: true}}}, {ID: "later", Definition: &selected, Join: "all", Depends: []Dependency{{Node: "human", On: []string{"approve"}, Required: true}}}}}
	oldDefinition := put("workflow-definition:apply-old", applyGraph)
	oldAdoption := adopt(oldDefinition)
	root = must(s.Start(ctx, admin, w, "workflow-execution:apply", Start{Mutation: mutation("0"), Adoption: oldAdoption}))
	step(2)
	report("first", "")
	step(2)
	_, beforeDecision, _ := execution(ctx, s.Store.Database(), w, root.ID)
	gate := state()["human"]
	must(s.Decide(ctx, editor, w, root.ID, Decision{Mutation: mutation(beforeDecision.Version), Node: "human", Attempt: gate.Attempt, Choice: "approve"}))
	started := state()["first"]
	startedInput, err := s.Runs.Read(ctx, admin, w, *started.Run)
	if err != nil {
		t.Fatal(err)
	}
	text = "new Context"
	contextVersion = must(s.Contexts.Put(ctx, editor, w, contexts.Identity{Kind: "sentence", ID: "workflow-context"}, contexts.Write{Mutation: mutation(contextVersion.Version), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
	contextRef.Revision = contextVersion.Version
	baselineVersion = must(s.Contexts.Put(ctx, editor, w, contexts.Identity{Kind: "baseline", ID: "workflow-baseline"}, contexts.Write{Mutation: mutation(baselineVersion.Version), Value: raw(contexts.Baseline{Contexts: []contexts.Ref{contextRef}})}))
	baseline.Revision = baselineVersion.Version
	selectedDef.Selection = &contexts.Selection{Mode: "select", Include: []contexts.Ref{contextRef}}
	selectedNew := put("run-definition:selected-v2", selectedDef)
	applyGraph.Baseline = &baseline
	applyGraph.Nodes[2].Definition = &selectedNew
	nextDefinition := put("workflow-definition:apply-new", applyGraph)
	nextAdoption := adopt(nextDefinition)
	current, currentRoot, _ := execution(ctx, s.Store.Database(), w, root.ID)
	if current.Definition != oldDefinition || state()["later"].Definition == nil || *state()["later"].Definition != selected {
		t.Fatal("editing changed live adoption")
	}
	impact, err := s.Impact(ctx, admin, w, ImpactRequest{Execution: currentRoot.Ref, Adoption: nextAdoption})
	if err != nil || len(impact.Affected) != 2 || len(impact.Invalidated) != 1 || impact.Invalidated[0] != "human" {
		t.Fatal("apply impact", impact, err)
	}
	must(s.Apply(ctx, admin, w, root.ID, Apply{Mutation: mutation(currentRoot.Version), Impact: impact}))
	if state()["human"].DecisionAt != nil || state()["first"].Run.ID != started.Run.ID {
		t.Fatal("apply mutated started Run or retained stale decision")
	}
	retained, err := s.Runs.Read(ctx, admin, w, *started.Run)
	if err != nil || string(retained.Value) != string(startedInput.Value) {
		t.Fatal("started fixed Run changed")
	}
	step(1)
	_, newRoot, _ := execution(ctx, s.Store.Database(), w, root.ID)
	gate = state()["human"]
	must(s.Decide(ctx, editor, w, root.ID, Decision{Mutation: mutation(newRoot.Version), Node: "human", Attempt: gate.Attempt, Choice: "approve"}))
	step(2)
	if state()["later"].Prepared == nil || !strings.Contains(string(state()["later"].Prepared.Spec.Input.Context), "new Context") {
		t.Fatal("new Context not adopted for unstarted Run", state())
	}
	t.Log("fixed workflow+Run export, unchanged live adoption on edit, explicit affected unstarted inputs and stale decision invalidation passed")
}
func ptr(v int) *int { return &v }
