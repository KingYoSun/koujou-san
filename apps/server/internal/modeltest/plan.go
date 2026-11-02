// Package modeltest supplies a server-owned, offline ChatGPT plan fixture.
// No request in this fixture is sent to OpenAI or uses real credentials.
package modeltest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/chatgptplan"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func Plan(t *testing.T, config, target, key string) *serverllm.Gateway {
	t.Helper()
	file := filepath.Join(t.TempDir(), "credential.json")
	if err := chatgptplan.Save(file, chatgptplan.Credential{Issuer: chatgptplan.Issuer, Subject: "owned", ClientID: "owned-client", HostID: "urn:uuid:owned", IDToken: "owned-id-token", AccessToken: key, RefreshToken: "owned-refresh", TokenType: "Bearer", ExpiresIn: 3600, SavedAt: time.Now(), Scopes: []string{"chatgpt.tokens.use.direct", "resource.invoke", "offline_access"}}); err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal([]any{map[string]any{"type": "chatgpt", "id": "owned", "name": "Owned plan", "credential_file": file, "workspaces": []string{"one"}, "models": []serverllm.Model{{Requested: "owned-model", Actual: []string{"owned-model"}}}}})
	if err := os.WriteFile(config, value, 0600); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(target)
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.openai.com" {
			t.Error("unexpected plan target")
			return nil, serverllm.ErrUnavailable
		}
		copy := r.Clone(r.Context())
		address := *r.URL
		address.Scheme, address.Host = base.Scheme, base.Host
		copy.URL, copy.Host = &address, base.Host
		return http.DefaultTransport.RoundTrip(copy)
	})}
	return &serverllm.Gateway{ConfigFile: config, HTTP: client}
}

func Completed(w http.ResponseWriter, content, marker string) {
	w.Header().Set("Content-Type", "text/event-stream")
	value, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "owned-response", "model": "owned-model", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": content}}}}, "usage": map[string]int{"input_tokens": 23, "output_tokens": 9}, "owned_raw_marker": marker}})
	w.Write(append(append([]byte("data: "), value...), []byte("\n\n")...))
}
