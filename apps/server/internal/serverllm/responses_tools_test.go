package serverllm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponsesToolsKeepNamespacedCallsAndReasoningHistory(t *testing.T) {
	var envelope responseEnvelope
	if json.Unmarshal([]byte(`{"id":"owned","model":"owned-model","status":"completed","output":[{"type":"reasoning","id":"rs_owned","encrypted_content":"OPAQUE_REASONING","summary":[]},{"type":"function_call","id":"fc_owned","call_id":"call_owned","namespace":"koujou","name":"context_search","arguments":"{\"query\":\"owned\"}"}],"usage":{"input_tokens":12,"output_tokens":4}}`), &envelope) != nil {
		t.Fatal("fixture")
	}
	reply, reason := envelope.parsed()
	if reason != "" || len(reply.Choices) != 1 || len(reply.Choices[0].Message.Calls) != 1 || reply.Choices[0].Finish != "tool_calls" {
		t.Fatal(reply, reason)
	}
	message := reply.Choices[0].Message
	serialized, _ := json.Marshal(message)
	if !strings.Contains(string(serialized), `"role":"assistant"`) {
		t.Fatal("assistant role missing from common Host history")
	}
	if message.Calls[0].ID != "call_owned" || message.Calls[0].Function.Name != "context_search" || !strings.Contains(string(message.ServerOutput), "OPAQUE_REASONING") {
		t.Fatal("call or reasoning lost")
	}
	assistant, _ := json.Marshal(message)
	var prior map[string]any
	_ = json.Unmarshal(assistant, &prior)
	prior["role"] = "assistant"
	body, _ := json.Marshal(map[string]any{"model": "owned-model", "messages": []any{map[string]string{"role": "system", "content": "owned policy"}, map[string]string{"role": "user", "content": "owned question"}, prior, map[string]string{"role": "tool", "tool_call_id": "call_owned", "content": "{\"items\":[]}"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "context_search", "description": "Owned read tool", "parameters": map[string]any{"type": "object"}}}}})
	wire, err := responsesRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Input []json.RawMessage `json:"input"`
		Tools []struct {
			Type, Name, Description string
			Tools                   []struct{ Type, Name string }
		} `json:"tools"`
		Include []string `json:"include"`
		Store   bool     `json:"store"`
		Stream  bool     `json:"stream"`
	}
	if json.Unmarshal(wire, &sent) != nil || len(sent.Input) != 5 || sent.Store || !sent.Stream || len(sent.Tools) != 1 || sent.Tools[0].Type != "namespace" || sent.Tools[0].Name != "koujou" || sent.Tools[0].Description == "" || len(sent.Tools[0].Tools) != 1 || sent.Tools[0].Tools[0].Name != "context_search" || len(sent.Include) != 1 || sent.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("wire: %s", wire)
	}
	for i, expected := range envelope.Output {
		var actual, original any
		_ = json.Unmarshal(sent.Input[i+2], &actual)
		_ = json.Unmarshal(expected, &original)
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(original)
		if string(a) != string(b) {
			t.Fatal("original output item changed")
		}
	}
	if !strings.Contains(string(sent.Input[4]), `"function_call_output"`) || !strings.Contains(string(sent.Input[4]), `"call_owned"`) {
		t.Fatal("tool output lost")
	}
}
