package serverllm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/chatgptplan"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
)

func ownedCredential(t *testing.T) (string, chatgptplan.Credential) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "credential.json")
	c := chatgptplan.Credential{Issuer: chatgptplan.Issuer, Subject: "owned-subject", ClientID: "owned-client", HostID: "urn:uuid:owned", IDToken: "owned-id-token", AccessToken: "owned-access-token", RefreshToken: "owned-refresh-token", TokenType: "Bearer", ExpiresIn: 3600, SavedAt: time.Now(), Scopes: []string{"chatgpt.tokens.use.direct", "resource.invoke", "offline_access"}}
	if err := chatgptplan.Save(file, c); err != nil {
		t.Fatal(err)
	}
	return file, c
}

func TestResponsesDispatchPreservesWireStreamAndTerminalUsage(t *testing.T) {
	file, credential := ownedCredential(t)
	for _, state := range []string{"completed", "failed", "incomplete", "interrupted", "wrong-model", "timeout"} {
		t.Run(state, func(t *testing.T) {
			var calls atomic.Int32
			var sent string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer owned-access-token" || r.Header.Get("Cookie") != "" || r.Header.Get("x-bf-vk") != "" {
					t.Error("inference credential boundary changed")
				}
				var request map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("invalid wire request")
				}
				if string(request["store"]) != "false" || string(request["stream"]) != "true" || request["max_output_tokens"] != nil || request["max_completion_tokens"] != nil || request["fallbacks"] != nil {
					t.Error("preview or unlimited-output contract changed")
				}
				var messages []struct{ Role, Content string }
				json.Unmarshal(request["input"], &messages)
				if len(messages) != 2 || messages[0].Role != "developer" || messages[1].Content != "owned evidence" {
					t.Error("fixed input changed")
				}
				wire, _ := json.Marshal(request)
				sent = string(wire)
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("x-request-id", "owned-provider-request")
				fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"unmodified_event\":true}\n\n")
				w.(http.Flusher).Flush()
				if state == "timeout" {
					<-r.Context().Done()
					return
				}
				if state == "interrupted" {
					return
				}
				model, status := "actual-owned", state
				if state == "wrong-model" {
					model, status = "different-model", "completed"
				}
				body := map[string]any{"type": "response." + status, "response": map[string]any{"id": "response-owned", "model": model, "status": status, "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "{\"verdict\":false}"}}}}, "usage": map[string]any{"input_tokens": 7, "output_tokens": 12345, "input_tokens_details": map[string]int{"cached_tokens": 2}, "output_tokens_details": map[string]int{"reasoning_tokens": 3}}}}
				raw, _ := json.Marshal(body)
				fmt.Fprintf(w, "event: response.%s\ndata: %s\n\n", status, raw)
			}))
			defer upstream.Close()
			provider := chatgptplan.Default()
			provider.Resource = upstream.URL + "/v1"
			provider.HTTP = upstream.Client()
			connection := Connection{Type: "chatgpt", Endpoint: provider.Resource, CredentialFile: file, identity: credential.Identity(), provider: &provider}
			spec := &Spec{Allowed: []string{"actual-owned"}, UnlimitedOutput: true, Limits: Limits{InputBytes: 4096, Seconds: 1}}
			request := json.RawMessage(`{"model":"requested-owned","messages":[{"role":"system","content":"owned instructions"},{"role":"user","content":"owned evidence"}],"response_format":{"type":"json_schema","json_schema":{"name":"owned","strict":true,"schema":{"type":"object"}}}}`)
			saved := false
			err := Dispatch(context.Background(), connection, spec, request, "owned-intent", func(ctx context.Context, response Response, reply modelreply.Reply, reason string) error {
				saved = true
				if ctx.Err() != nil || response.Status != 200 || response.Protocol != "openai-responses" || response.RequestID != "owned-provider-request" {
					t.Error("completion did not retain transport evidence")
				}
				if string(response.Request) != sent {
					t.Error("sent request was not retained")
				}
				var original string
				if json.Unmarshal(response.Body, &original) != nil || !strings.Contains(original, "unmodified_event") {
					t.Error("raw SSE was replaced with a summary")
				}
				if state == "completed" {
					if reason != "" || reply.Model != "actual-owned" || reply.Usage.Output == nil || *reply.Usage.Output != 12345 || reply.Usage.Input == nil || *reply.Usage.Input != 7 {
						t.Error("completed response or unrestricted usage rejected")
					}
				} else if reason == "" {
					t.Error("non-terminal/failed/unpermitted model accepted")
				}
				if state == "failed" || state == "incomplete" {
					if reply.Usage.Output == nil || *reply.Usage.Output != 12345 {
						t.Error("failed attempt usage was lost")
					}
				}
				return nil
			})
			if err != nil || !saved || calls.Load() != 1 {
				t.Fatalf("dispatch: saved=%v calls=%d error=%v", saved, calls.Load(), err)
			}
		})
	}
}

func TestChatGPTConnectionRevisionAndCredentialRedirectBoundary(t *testing.T) {
	file, credential := ownedCredential(t)
	config := filepath.Join(t.TempDir(), "connections.json")
	entry := map[string]any{"type": "chatgpt", "id": "plan", "name": "Owned plan", "credential_file": file, "workspaces": []string{"one", "other-private-workspace"}, "models": []Model{{Requested: "requested", Actual: []string{"actual"}}}}
	raw, _ := json.Marshal([]any{entry})
	os.WriteFile(config, raw, 0600)
	gateway := Gateway{ConfigFile: config}
	list, err := gateway.Connections("one")
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(list)
	for _, secret := range []string{file, credential.AccessToken, credential.RefreshToken, credential.Subject, credential.ClientID, credential.HostID, "other-private-workspace"} {
		if strings.Contains(string(metadata), secret) {
			t.Fatal("private credential or grant leaked")
		}
	}
	revision := list[0].Revision
	credential.AccessToken = "rotated-access"
	chatgptplan.Save(file, credential)
	list, err = gateway.Connections("one")
	if err != nil || list[0].Revision != revision {
		t.Fatal("token rotation invalidated definition")
	}
	credential.Subject = "different-owner"
	chatgptplan.Save(file, credential)
	if _, err = gateway.Connection("one", "plan", revision); err == nil {
		t.Fatal("account replacement retained old revision")
	}
	var forwarded atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer redirect.Close()
	provider := chatgptplan.Default()
	provider.Resource = redirect.URL + "/v1"
	provider.HTTP = redirect.Client()
	connection := Connection{Type: "chatgpt", Endpoint: provider.Resource, CredentialFile: file, identity: credential.Identity(), provider: &provider}
	response, err := connection.Call(context.Background(), json.RawMessage(`{"model":"requested","messages":[{"role":"user","content":"owned"}]}`), "intent")
	if err != nil || response.Status != 302 || forwarded.Load() != 0 {
		t.Fatal("redirect received a credential")
	}
	profiles, err := gateway.Connections("not-granted")
	if err != nil || len(profiles) != 0 {
		t.Fatal("workspace boundary changed")
	}
}
