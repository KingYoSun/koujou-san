package automation

import (
	"encoding/json"
	"testing"
)

// The policy saved by the shared UI must retain its model contract while old
// policies remain readable. This is the boundary, not a copy of the evaluator.
func TestTriagePolicyModelContract(t *testing.T) {
	base := `{"name":"delivery","scopes":["project"],"precedence":1,"levels":[{"id":"P1","name":"high","meaning":"urgent"}],"criteria":"impact and deadline","context":[]`
	var p Policy
	if err := decode([]byte(base+`}`), &p); err != nil {
		t.Fatal(err)
	}
	model := `,"model":{"connection":"owned","revision":"fixed","model":"owned-model","allowed_models":["owned-model"],"fallbacks":[],"instructions":"Use the policy","context":{"mode":"none","include":[],"exclude":[]},"evidence":[],"output_channels":[],"limits":{"input_bytes":1048576,"output_tokens":4096,"seconds":120},"reevaluate_evidence":true}}`
	if err := decode([]byte(base+model), &p); err != nil {
		t.Fatalf("new triage policy cannot be saved: %v", err)
	}
	saved, _ := json.Marshal(p)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(saved, &fields)
	if fields["model"] == nil {
		t.Fatal("model selection lost while saving")
	}
}
