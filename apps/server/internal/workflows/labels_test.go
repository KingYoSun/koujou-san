package workflows

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLabelDoesNotReturnDefinitionBody(t *testing.T) {
	record := Record{Ref: Ref{ID: "run-definition:test", Version: "fixed"}, Kind: "run-definition", Value: json.RawMessage(`{"name":"検品","inputs":["prompt"],"spec":{"input":{"command":{"parameters":{"secret":"private body","integer":900719925474099312345}}}},"unknown":900719925474099312345}`)}
	before := string(record.Value)
	label := labelOf(record)
	encoded, err := json.Marshal(label)
	if err != nil || label.Name != "検品" || len(label.Inputs) != 1 || label.Inputs[0] != "prompt" || label.Ref != record.Ref {
		t.Fatalf("wrong label: %s, %v", encoded, err)
	}
	if strings.Contains(string(encoded), "private body") || strings.Contains(string(encoded), "900719") || string(record.Value) != before {
		t.Fatal("label leaked or changed the original definition")
	}
}
