package serverllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfiguredGatewayKeepsKeysAndWorkspaceGrantsOutOfMetadataAndDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer other.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-bf-vk") != "owned-gateway-key" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("x-request-id") != "owned-intent" {
			t.Errorf("unexpected credential or request binding")
		}
		if r.Header.Get("x-bf-cache-no-store") != "true" || r.Header.Get("x-bf-disable-content-logging") != "true" {
			t.Error("missing request privacy options")
		}
		if r.URL.Path == "/redirect/chat/completions" {
			w.Header().Set("Location", other.URL)
			w.WriteHeader(302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-bifrost-request-id", "owned-intent")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"owned-failure"},"usage":{"prompt_tokens":12}}`))
	}))
	defer upstream.Close()
	dir := t.TempDir()
	key := filepath.Join(dir, "private-key")
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(key, []byte("owned-gateway-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	write := func(endpoint string) {
		b, _ := json.Marshal([]any{map[string]any{"id": "owned", "name": "Owned gateway", "endpoint": endpoint, "key_file": key, "workspaces": []string{"one", "private-other-workspace"}, "models": []Model{{Requested: "openai/owned", Actual: []string{"owned"}}}}})
		if err := os.WriteFile(config, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(upstream.URL + "/v1")
	g := Gateway{ConfigFile: config}
	profiles, err := g.Connections("one")
	if err != nil || len(profiles) != 1 {
		t.Fatalf("profiles: %v %v", profiles, err)
	}
	metadata, _ := json.Marshal(profiles)
	for _, private := range []string{key, "owned-gateway-key", "private-other-workspace"} {
		if strings.Contains(string(metadata), private) {
			t.Fatal("private configuration leaked in metadata")
		}
	}
	if profiles, err := g.Connections("unregistered"); err != nil || len(profiles) != 0 {
		t.Fatalf("unregistered workspace: %v %v", profiles, err)
	}
	p := profiles[0]
	if !p.Allows("openai/owned", []string{"owned"}) || p.Allows("openai/owned", []string{"other"}) {
		t.Fatal("model boundary changed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := p.Call(ctx, json.RawMessage(`{"model":"openai/owned","messages":[]}`), "owned-intent")
	if err != nil || r.Status != 503 || r.RequestID != "owned-intent" || !strings.Contains(string(r.Body), "owned-failure") {
		t.Fatalf("raw failure not retained: %#v %v", r, err)
	}
	write(upstream.URL + "/redirect")
	if _, err := g.Connection("one", p.ID, p.Revision); err == nil {
		t.Fatal("stale connection revision accepted")
	}
	profiles, err = g.Connections("one")
	if err != nil {
		t.Fatal(err)
	}
	r, err = profiles[0].Call(ctx, json.RawMessage(`{}`), "owned-intent")
	if err != nil || r.Status != 302 || redirected.Load() != 0 {
		t.Fatalf("redirect forwarded a credential: %v %v", r, err)
	}
}
