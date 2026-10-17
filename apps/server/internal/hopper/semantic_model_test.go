package hopper

import (
	"encoding/json"
	"testing"
)

func TestSemanticResponseRequiresCandidateKey(t *testing.T) {
	for _, candidate := range []string{"", `,"candidate":null`} {
		var decision SemanticDecision
		err := json.Unmarshal([]byte(`{"verdict":"false","reason":"no finding","evidence":[],"unknown":[]`+candidate+`}`), &decision)
		valid := err == nil && decision.validate(SemanticInput{})
		if valid != (candidate != "") {
			t.Fatalf("candidate %q: accepted=%v, error=%v", candidate, valid, err)
		}
	}
}
