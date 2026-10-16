package runs

import "testing"

func TestCommandAndInspectionContracts(t *testing.T) {
	command := Command{Kind: "cli", Target: "go", Parameters: raw([]string{"test", "./..."})}
	if validate(Spec{Input: Input{Command: command}}) != nil {
		t.Fatal("command-only input rejected")
	}
	if validate(Spec{}) == nil {
		t.Fatal("missing command accepted")
	}
	if validate(Spec{Input: Input{Command: command, Context: raw(map[string]int{"x": 1})}}) == nil {
		t.Fatal("specified input silently discarded without binding")
	}
	command.Binding = map[string]string{"context": "stdin"}
	if validate(Spec{Input: Input{Command: command, Context: raw(map[string]int{"x": 1})}}) != nil {
		t.Fatal("arbitrary Context value rejected")
	}
	contract := &Contract{Name: "inspection", Version: "v1", ResultSchema: raw(map[string]any{"type": "object", "required": []string{"decision"}}), VerdictField: "decision"}
	for _, report := range []any{map[string]string{"decision": "FAIL"}, map[string]string{"decision": "INCONCLUSIVE"}} {
		v := assess(contract, raw(report))
		if v.State != "valid" || v.Verdict == "PASS" {
			t.Fatal(v)
		}
	}
	for _, report := range []any{nil, "PASS", map[string]string{"summary": "looks good"}, map[string]string{"decision": "maybe"}} {
		v := assess(contract, raw(report))
		if v.State != "invalid" || v.Verdict != "" {
			t.Fatal("invalid output became usable verdict", v)
		}
	}
	contract.ResultSchema = raw(map[string]string{"$ref": "https://example.test/remote-schema"})
	if validate(Spec{Input: Input{Command: command}, Derivation: contract}) == nil {
		t.Fatal("external schema reference fetched or accepted")
	}
}
