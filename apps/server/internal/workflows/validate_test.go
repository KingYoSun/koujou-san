package workflows

import (
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"testing"
)

func plainDefinition() Definition {
	return Definition{Schema: "run-definition-v1", Name: "fixed command", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture"}}}}
}
func graphDefinition() Definition {
	a, b := Ref{ID: "run-definition:plain", Version: "fixed"}, Ref{ID: "run-definition:inspection", Version: "fixed"}
	return Definition{Schema: "workflow-definition-v1", Name: "fixed graph", Budget: &Budget{MaxRuns: 8, Parallel: 2}, Nodes: []Node{{ID: "implementation", Definition: &a}, {ID: "inspection", Definition: &b, Join: "all", Depends: []Dependency{{Node: "implementation", On: []string{"succeeded"}, Required: true}}, Retry: map[string]string{"FAIL": "implementation"}}, {ID: "human", Choices: []string{"approve", "rework"}, Join: "all", Depends: []Dependency{{Node: "inspection", On: []string{"INCONCLUSIVE"}, Required: true}}}}}
}
func TestDefinitionValidationDoesNotExecuteAndFindsDeclaredLocations(t *testing.T) {
	run := plainDefinition()
	resolve := func(r Ref) (Definition, error) { return run, nil }
	good := graphDefinition()
	if problems := validate(good, resolve); len(problems) != 0 {
		t.Fatal(problems)
	}
	tests := []struct {
		name, code string
		change     func(*Definition)
	}{
		{"missing return", "return_missing", func(d *Definition) { d.Nodes[1].Retry["FAIL"] = "absent" }},
		{"unbounded retry", "finite_budget_required", func(d *Definition) { d.Budget.MaxRuns = 0 }},
		{"unconnected input", "input_unconnected", func(d *Definition) { d.Nodes[1].Definition = &Ref{ID: "run-definition:input", Version: "fixed"} }},
		{"unknown producer", "dependency_missing", func(d *Definition) { d.Nodes[1].Depends[0].Node = "absent" }},
		{"cycle", "dependency_cycle", func(d *Definition) {
			d.Nodes[0].Depends = []Dependency{{Node: "inspection", On: []string{"PASS"}, Required: true}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Definition
			_ = json.Unmarshal(raw(good), &d)
			tt.change(&d)
			problems := validate(d, func(ref Ref) (Definition, error) {
				v := run
				if ref.ID == "run-definition:input" {
					v.Inputs = []string{"prompt"}
				}
				return v, nil
			})
			found := false
			for _, p := range problems {
				if p.Code == tt.code && p.Path != "" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing location/code", tt.code, problems)
			}
		})
	}
}
func TestSelectedValuePreservesTypeAndDoesNotReadOtherOutputs(t *testing.T) {
	original := json.RawMessage(`{"selected":{"number":123456789012345678901234567890,"value":"固定値"},"unselected":"NEVER_INJECT"}`)
	selected, err := selectValue(original, []string{"selected", "number"})
	if err != nil || string(selected) != "123456789012345678901234567890" {
		t.Fatal(string(selected), err)
	}
	_, err = selectValue(original, []string{"missing"})
	if err == nil {
		t.Fatal("missing input accepted")
	}
}
