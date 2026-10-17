package workflows

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func validate(d Definition, resolve func(Ref) (Definition, error)) []Problem {
	problems := []Problem{}
	add := func(path, code string) { problems = append(problems, Problem{path, code}) }
	if strings.TrimSpace(d.Name) == "" {
		add("/name", "name_required")
	}
	switch d.Schema {
	case "run-definition-v1":
		if d.Spec == nil {
			add("/spec", "spec_required")
			break
		}
		if d.Spec.Input.Command.Kind == "" || d.Spec.Input.Command.Target == "" {
			add("/spec/input/command", "command_required")
		}
		for _, input := range d.Inputs {
			if !inputName(input) {
				add("/inputs", "input_target_invalid")
			}
		}
		candidate := *d.Spec
		if d.Spec.Manifest != nil {
			add("/spec/manifest", "selection_required_for_new_run")
		}
		for _, input := range d.Inputs {
			if input == "context" && len(candidate.Input.Context) == 0 {
				candidate.Input.Context = raw(nil)
			}
			if input == "prompt" && len(candidate.Input.Prompt) == 0 {
				candidate.Input.Prompt = raw(nil)
			}
		}
		if runs.ValidateSpec(candidate) != nil {
			add("/spec", "invalid_run_contract")
		}
		for _, name := range []string{"context", "prompt"} {
			present := len(d.Spec.Input.Context) > 0
			if name == "prompt" {
				present = len(d.Spec.Input.Prompt) > 0
			}
			if present && d.Spec.Input.Command.Binding[name] == "" {
				add("/spec/input/command/binding/"+name, "binding_required")
			}
		}
		if d.Selection != nil && d.Selection.Mode != "none" && d.Selection.Mode != "select" && d.Selection.Mode != "" {
			add("/selection/mode", "selection_invalid")
		}
		if d.Selection != nil && (len(d.Spec.Input.Context) > 0 || d.Spec.Manifest != nil) {
			add("/selection", "context_sources_conflict")
		}
		if d.Write != nil && (!d.Write.Conditional || d.Write.Target == "" || d.Write.Commit == "" || len(d.Write.Required) == 0) {
			add("/write", "conditional_write_guard_required")
		}
		if d.Write != nil {
			value, err := selectValue(d.Spec.Input.Command.Parameters, d.Write.Parameter)
			if d.Write.Parameter == nil || err != nil || string(value) != string(raw(d.Write.Commit)) {
				add("/write/parameter", "conditional_parameter_required")
			}
		}
		if d.CI != nil && (d.CI.Target == "" || d.CI.Commit == "" || d.CI.RunID == "" || len(d.CI.RequiredJobs) == 0) {
			add("/ci", "fixed_ci_target_required")
		}
		if len(d.Nodes) > 0 || d.Budget != nil {
			add("/nodes", "definition_type_mismatch")
		}
	case "workflow-definition-v1":
		if len(d.Nodes) == 0 {
			add("/nodes", "nodes_required")
		}
		if d.Budget == nil || d.Budget.Parallel < 1 || d.Budget.MaxRuns < 0 || d.Budget.Seconds < 0 {
			add("/budget", "explicit_budget_required")
		}
		if d.Budget != nil && d.Budget.Cost != nil {
			c := d.Budget.Cost
			amount, ok := new(big.Rat).SetString(c.Amount)
			if !ok || !regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`).MatchString(c.Amount) || amount.Sign() < 0 || c.Currency == "" || (c.Valuation != "reported" && c.Valuation != "estimated" && c.Valuation != "api_equivalent" && c.Valuation != "allocated") {
				add("/budget/cost", "cost_budget_invalid")
			}
		}
		if d.Spec != nil || d.Selection != nil || d.CI != nil || d.Write != nil {
			add("/spec", "definition_type_mismatch")
		}
		byID := map[string]Node{}
		for i, n := range d.Nodes {
			path := fmt.Sprintf("/nodes/%d", i)
			if n.ID == "" {
				add(path+"/id", "node_id_required")
			}
			if _, exists := byID[n.ID]; exists {
				add(path+"/id", "node_duplicate")
			}
			byID[n.ID] = n
		}
		for i, n := range d.Nodes {
			path := fmt.Sprintf("/nodes/%d", i)
			if (n.Definition == nil) == (len(n.Choices) == 0) {
				add(path, "run_or_decision_required")
			}
			if len(n.Depends) > 0 && n.Join != "all" && n.Join != "any" {
				add(path+"/join", "join_policy_required")
			}
			deps := map[string]bool{}
			for j, dep := range n.Depends {
				p := fmt.Sprintf("%s/depends/%d", path, j)
				if _, ok := byID[dep.Node]; !ok {
					add(p, "dependency_missing")
				}
				if len(dep.On) == 0 {
					add(p+"/on", "result_condition_required")
				}
				deps[dep.Node] = true
			}
			supplied := map[string]bool{}
			for _, input := range n.Inputs {
				supplied[input] = true
				if !inputName(input) {
					add(path+"/inputs", "input_target_invalid")
				}
			}
			for j, b := range n.Bindings {
				p := fmt.Sprintf("%s/bindings/%d", path, j)
				if !deps[b.From] {
					add(p+"/from", "input_dependency_required")
				}
				if !inputName(b.To) || b.Path == nil || b.Index < 0 || (b.Kind != "output" && b.Kind != "result") || (b.Kind == "output" && b.Channel == "") {
					add(p, "output_selection_invalid")
				}
				if supplied[b.To] && b.To != "context" {
					add(p+"/to", "input_conflict")
				}
				supplied[b.To] = true
			}
			if n.Definition != nil {
				if !fixed(*n.Definition) {
					add(path+"/definition", "fixed_definition_required")
				} else {
					run, err := resolve(*n.Definition)
					if err != nil || run.Schema != "run-definition-v1" {
						add(path+"/definition", "definition_unavailable")
					} else {
						if run.Write != nil {
							for _, required := range run.Write.Required {
								if !deps[required.Node] || len(required.On) == 0 {
									add(path+"/definition/write/required", "judgment_unconnected")
								}
							}
						}
						for _, problem := range validate(run, resolve) {
							add(path+"/definition"+problem.Path, problem.Code)
						}
						for _, input := range run.Inputs {
							if !supplied[input] {
								add(path+"/bindings", "input_unconnected")
							}
						}
						for _, b := range n.Bindings {
							if b.To == "context" && run.Selection != nil && run.Selection.Mode == "none" {
								add(path+"/bindings", "none_context_binding")
							}
						}
					}
				}
			}
			for on, target := range n.Retry {
				if on == "" {
					add(path+"/retry", "result_condition_required")
				}
				if _, ok := byID[target]; !ok {
					add(path+"/retry", "return_missing")
				}
				if targetNode, ok := byID[target]; ok && targetNode.Definition == nil {
					add(path+"/retry", "return_run_required")
				}
				if d.Budget == nil || d.Budget.MaxRuns < 1 {
					add(path+"/retry", "finite_budget_required")
				}
			}
		}
		color := map[string]int{}
		var visit func(string) bool
		visit = func(id string) bool {
			if color[id] == 1 {
				return true
			}
			if color[id] == 2 {
				return false
			}
			color[id] = 1
			for _, dep := range byID[id].Depends {
				if _, ok := byID[dep.Node]; ok && visit(dep.Node) {
					return true
				}
			}
			color[id] = 2
			return false
		}
		for i, n := range d.Nodes {
			if visit(n.ID) {
				add(fmt.Sprintf("/nodes/%d/depends", i), "dependency_cycle")
				break
			}
		}
	default:
		add("/schema", "schema_invalid")
	}
	return problems
}
func inputName(name string) bool { return name == "context" || name == "prompt" || name == "command" }

// Each selected value remains RawMessage, including JSON numbers larger than a
// float64. Path is an explicit key/index sequence, never an evaluated expression.
func selectValue(value json.RawMessage, path []string) (json.RawMessage, error) {
	for _, key := range path {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) == nil && object != nil {
			v, ok := object[key]
			if !ok {
				return nil, ErrInvalid
			}
			value = v
			continue
		}
		var array []json.RawMessage
		index, err := strconv.Atoi(key)
		if err != nil || json.Unmarshal(value, &array) != nil || index < 0 || index >= len(array) {
			return nil, ErrInvalid
		}
		value = array[index]
	}
	if !json.Valid(value) {
		return nil, ErrInvalid
	}
	return value, nil
}
