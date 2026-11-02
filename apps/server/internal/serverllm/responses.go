package serverllm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/chatgptplan"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
)

// RequestBytes measures the wire body before the owner accepts its fixed input.
func RequestBytes(spec *Spec, body json.RawMessage) (int, error) {
	if !spec.UnlimitedOutput {
		return len(body), nil
	}
	wire, err := responsesRequest(body)
	return len(wire), err
}

func responsesRequest(body json.RawMessage) (json.RawMessage, error) {
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role         string            `json:"role"`
			Content      *string           `json:"content"`
			CallID       string            `json:"tool_call_id"`
			ServerOutput []json.RawMessage `json:"server_output"`
			Calls        []struct {
				ID       string                           `json:"id"`
				Type     string                           `json:"type"`
				Function struct{ Name, Arguments string } `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name, Description string
				Parameters        json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		MaxTokens int      `json:"max_completion_tokens"`
		Fallbacks []string `json:"fallbacks"`
		Format    struct {
			Type   string `json:"type"`
			Schema struct {
				Name   string          `json:"name"`
				Strict bool            `json:"strict"`
				Schema json.RawMessage `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if json.Unmarshal(body, &request) != nil || request.Model == "" || len(request.Messages) == 0 || request.MaxTokens != 0 || len(request.Fallbacks) != 0 {
		return nil, ErrUnavailable
	}
	input := []any{}
	for _, message := range request.Messages {
		if message.Role == "tool" {
			if message.CallID == "" || message.Content == nil {
				return nil, ErrUnavailable
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.CallID, "output": *message.Content})
			continue
		}
		if message.Role == "assistant" && len(message.ServerOutput) > 0 {
			for _, output := range message.ServerOutput {
				var item struct{ Type string }
				if json.Unmarshal(output, &item) != nil || (item.Type != "reasoning" && item.Type != "message" && item.Type != "function_call") {
					return nil, ErrUnavailable
				}
				input = append(input, output)
			}
			continue
		}
		role := message.Role
		if role == "system" {
			role = "developer"
		}
		if role != "developer" && role != "user" && role != "assistant" {
			return nil, ErrUnavailable
		}
		if message.Content != nil {
			input = append(input, map[string]string{"role": role, "content": *message.Content})
		}
		for _, call := range message.Calls {
			if role != "assistant" || call.Type != "function" || call.ID == "" || call.Function.Name == "" || !json.Valid([]byte(call.Function.Arguments)) {
				return nil, ErrUnavailable
			}
			input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "namespace": "koujou", "arguments": call.Function.Arguments})
		}
	}
	value := map[string]any{"model": request.Model, "input": input, "store": false, "stream": true}
	if len(request.Tools) > 0 {
		tools := []any{}
		for _, tool := range request.Tools {
			if tool.Type != "function" || tool.Function.Name == "" || !json.Valid(tool.Function.Parameters) {
				return nil, ErrUnavailable
			}
			tools = append(tools, map[string]any{"type": "function", "name": tool.Function.Name, "description": tool.Function.Description, "parameters": tool.Function.Parameters, "strict": false})
		}
		value["tools"] = []any{map[string]any{"type": "namespace", "name": "koujou", "description": "Read-only tools for the current operation.", "tools": tools}}
		value["include"] = []string{"reasoning.encrypted_content"}
	}
	if request.Format.Type != "" {
		if request.Format.Type != "json_schema" || request.Format.Schema.Name == "" || !json.Valid(request.Format.Schema.Schema) {
			return nil, ErrUnavailable
		}
		value["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": request.Format.Schema.Name, "strict": request.Format.Schema.Strict, "schema": request.Format.Schema.Schema}}
	}
	return json.Marshal(value)
}

type responseEnvelope struct {
	ID, Model, Status string
	Output            []json.RawMessage
	Usage             struct {
		Input        *uint64 `json:"input_tokens"`
		Output       *uint64 `json:"output_tokens"`
		InputDetails struct {
			Cached *uint64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			Reasoning *uint64 `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	}
}

func (e responseEnvelope) parsed() (*modelreply.Reply, string) {
	var text strings.Builder
	calls := []any{}
	reason := ""
	for _, output := range e.Output {
		var item struct {
			Type, Role, Name, Namespace, Arguments string
			CallID                                 string `json:"call_id"`
			Content                                []struct{ Type, Text string }
		}
		if json.Unmarshal(output, &item) != nil {
			reason = "invalid_model_response"
			continue
		}
		if item.Type == "reasoning" {
			continue
		}
		if item.Type == "function_call" {
			if item.CallID == "" || item.Name == "" || (item.Namespace != "" && item.Namespace != "koujou") || !json.Valid([]byte(item.Arguments)) {
				reason = "invalid_model_response"
				continue
			}
			calls = append(calls, map[string]any{"id": item.CallID, "type": "function", "function": map[string]string{"name": item.Name, "arguments": item.Arguments}})
			continue
		}
		if item.Type != "message" || item.Role != "assistant" {
			reason = "invalid_model_response"
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text.WriteString(part.Text)
			} else {
				reason = "incomplete_model_response"
			}
		}
	}
	finish := "stop"
	message := map[string]any{"role": "assistant", "content": text.String()}
	if len(calls) > 0 {
		finish = "tool_calls"
		message["tool_calls"], message["server_output"] = calls, e.Output
	}
	if e.Status != "completed" {
		finish = "length"
	}
	value := map[string]any{"id": e.ID, "model": e.Model, "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": e.Usage.Input, "completion_tokens": e.Usage.Output, "prompt_tokens_details": map[string]any{"cached_tokens": e.Usage.InputDetails.Cached}, "completion_tokens_details": map[string]any{"reasoning_tokens": e.Usage.OutputDetails.Reasoning}}}
	b, _ := json.Marshal(value)
	var reply modelreply.Reply
	_ = json.Unmarshal(b, &reply)
	return &reply, reason
}

func (p Connection) responses(ctx context.Context, body json.RawMessage, requestID string) (Response, error) {
	result := Response{Protocol: "openai-responses"}
	provider := chatgptplan.Default()
	if p.provider != nil {
		provider = *p.provider
	}
	if p.Endpoint != provider.Resource {
		return result, ErrUnavailable
	}
	request, err := responsesRequest(body)
	if err != nil {
		return result, err
	}
	result.Request = request
	credential, err := provider.Access(ctx, p.CredentialFile)
	if err != nil || p.identity == "" || credential.Identity() != p.identity {
		return result, ErrUnavailable
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if provider.HTTP != nil {
		client = *provider.HTTP
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if p.httpClient != nil {
		client = *p.httpClient
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.Endpoint+"/responses", bytes.NewReader(request))
	if err != nil {
		return result, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Client-Request-Id", requestID)
	response, err := client.Do(req)
	if err != nil {
		return result, ErrUnavailable
	}
	defer response.Body.Close()
	result.Status, result.RequestID = response.StatusCode, response.Header.Get("x-request-id")
	if response.StatusCode != 200 {
		raw, err := io.ReadAll(response.Body)
		if json.Valid(raw) {
			result.Body = raw
		} else {
			result.Body, _ = json.Marshal(string(raw))
		}
		return result, err
	}
	var original bytes.Buffer
	reader := bufio.NewReader(io.TeeReader(response.Body, &original))
	data := []string{}
	terminal := false
	for !terminal {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if (line == "" || readErr != nil) && len(data) > 0 {
			payload := strings.Join(data, "\n")
			data = nil
			var event struct {
				Type     string           `json:"type"`
				Response responseEnvelope `json:"response"`
			}
			if payload == "[DONE]" {
				break
			}
			if json.Unmarshal([]byte(payload), &event) != nil {
				result.Error = "invalid_model_response"
				break
			}
			switch event.Type {
			case "response.completed", "response.failed", "response.incomplete":
				result.Terminal = event.Type
				result.Reply, result.Error = event.Response.parsed()
				terminal = true
				if event.Type != "response.completed" || event.Response.Status != "completed" {
					result.Error = "incomplete_model_response"
				}
			case "error":
				result.Terminal = event.Type
				result.Error = "model_request_failed"
				terminal = true
			}
		}
		if readErr != nil {
			break
		}
	}
	if !terminal && result.Error == "" {
		result.Error = "incomplete_model_response"
	}
	result.Body, _ = json.Marshal(original.String())
	return result, nil
}
