package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/hopper"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
)

func prepareSemanticHopperUI(t *testing.T, ctx context.Context, s *Service, p access.Principal, source string) {
	t.Helper()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string }
		}
		if r.Header.Get("x-bf-vk") != "owned-ui-key" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 {
			http.Error(w, "owned request invalid", 400)
			return
		}
		var evidence []hopper.SemanticEvidence
		_ = json.Unmarshal([]byte(request.Messages[1].Content), &evidence)
		if len(evidence) == 0 {
			http.Error(w, "owned evidence absent", 400)
			return
		}
		decision := hopper.SemanticDecision{Verdict: "true", Reason: "元の結果に見送り問題が明記されています。", Candidate: &hopper.Finding{Identity: "owned-ui-deferred", Title: "合成の見送り問題", Body: "別の作業として再現を確認してください。", Status: "needs_reproduction"}, Evidence: []hopper.Citation{{Ref: evidence[0].Ref, Quote: "OWNED_UI_DEFERRED"}}, Unknown: []string{}}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-bifrost-request-id", r.Header.Get("x-request-id"))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-ui-response", "model": "owned-ui-model", "choices": []any{map[string]any{"message": map[string]any{"content": string(encodeTest(decision))}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 21, "completion_tokens": 8}})
	}))
	t.Cleanup(model.Close)
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	config := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(key, []byte("owned-ui-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, encodeTest([]any{map[string]any{"id": "owned-ui", "name": "受入: 所有モデルHTTP", "endpoint": model.URL + "/v1", "key_file": key, "workspaces": []string{"mcp-one"}, "models": []serverllm.Model{{Requested: "openai/owned-ui-model", Actual: []string{"owned-ui-model"}}}}}), 0600); err != nil {
		t.Fatal(err)
	}
	s.Hopper.Gateway = &serverllm.Gateway{ConfigFile: config}
	connections, err := s.Hopper.Gateway.Connections("mcp-one")
	if err != nil {
		t.Fatal(err)
	}
	put := func(id string, value any) access.VersionRef {
		t.Helper()
		v, err := s.Workflows.Put(ctx, p, "mcp-one", id, workflows.Write{Mutation: apicontract.Mutation{OperationID: "owned-semantic-ui-template", ExpectedVersion: "0"}, Value: encodeTest(value)})
		if err != nil || v.Data == nil {
			t.Fatal(v, err)
		}
		return access.VersionRef{ID: v.Data.ID, Version: v.Data.Version}
	}
	template := put("run-definition:semantic-ui", workflows.Definition{Schema: "run-definition-v1", Name: "受入: 見送り問題の再現", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture", Binding: map[string]string{"prompt": "stdin-json"}}}}, Selection: &contexts.Selection{Mode: "none"}})
	d := hopper.Template()
	d.Name = "受入: サーバー意味判断"
	d.Mode = "proposal"
	d.Problem = nil
	d.Targets = []hopper.Target{{Kind: "run", ID: source}}
	d.Condition = hopper.Condition{All: []hopper.Condition{{Field: "run.state", Op: "eq", Value: json.RawMessage(`"failed"`)}, {Semantic: true}}}
	d.Action.Template = template
	d.Action.Mappings = []hopper.Mapping{{Path: []string{"semantic", "candidate"}, To: "prompt"}}
	d.Action.Start = false
	sem := hopper.DefaultSemantic()
	sem.Connection = connections[0].ID
	sem.Revision = connections[0].Revision
	sem.Model = "openai/owned-ui-model"
	sem.Allowed = []string{"owned-ui-model"}
	sem.OutputChannels = []string{}
	d.Semantic = &sem
	v, err := s.Hopper.Put(ctx, p, "mcp-one", "hopper:ui-semantic", hopper.Write{Mutation: apicontract.Mutation{OperationID: "owned-semantic-ui-definition", ExpectedVersion: "0"}, Value: encodeTest(d)})
	if err != nil || v.Data == nil {
		t.Fatal(v, err)
	}
}
