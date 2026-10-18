package mcpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
)

func TestDefinitionModelMetadataDoesNotContainEmbeddedInput(t *testing.T) {
	definition := workflows.Definition{Schema: "run-definition-v1", Name: "Readable template", DeviceID: "owned", Spec: &runs.Spec{Input: runs.Input{Context: json.RawMessage(`"private-context"`), Prompt: json.RawMessage(`"private-prompt"`), Command: runs.Command{Kind: "cli", Target: "owned", Parameters: json.RawMessage(`["private-argument"]`)}}}}
	payload, _ := json.Marshal(definition)
	value, err := definitionMetadata(workflows.Record{Ref: workflows.Ref{ID: "run-definition:owned", Version: "v1"}, Kind: "run-definition", Value: payload})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(value)
	if strings.Contains(string(wire), "private-") || !strings.Contains(string(wire), `"metadata_only":true`) || !strings.Contains(string(wire), "Readable template") {
		t.Fatal("template metadata crossed the input boundary")
	}
	if _, err := definitionMetadata(workflows.Record{Kind: "execution", Value: payload}); err == nil {
		t.Fatal("execution body accepted as template metadata")
	}
}
