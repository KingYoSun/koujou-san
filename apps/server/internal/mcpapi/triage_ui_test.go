package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

func prepareTriageUI(t *testing.T, ctx context.Context, s *Service, p access.Principal) {
	t.Helper()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		if r.Header.Get("x-bf-vk") != "owned-triage-ui" || json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) != 2 {
			http.Error(w, "owned triage request invalid", 400)
			return
		}
		var input struct {
			Evidence []automation.Evidence `json:"evidence"`
		}
		if json.Unmarshal([]byte(req.Messages[1].Content), &input) != nil || len(input.Evidence) == 0 {
			http.Error(w, "owned input absent", 400)
			return
		}
		value := "P1"
		decision := automation.Decision{State: "evaluated", Priority: &value, Reason: "合成障害による利用者への影響を根拠に、高を推奨します。", Evidence: []automation.Citation{{Ref: input.Evidence[0].Ref, Quote: "合成障害"}}, Unknown: []string{"工数と影響人数は未計測です"}}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-triage-ui-response", "model": "owned-triage-ui-model", "choices": []any{map[string]any{"message": map[string]any{"content": string(encodeTest(decision))}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 23, "completion_tokens": 9, "prompt_tokens_details": map[string]int{"cached_tokens": 0}, "completion_tokens_details": map[string]int{"reasoning_tokens": 0}}})
	}))
	t.Cleanup(model.Close)
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	config := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(key, []byte("owned-triage-ui"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, encodeTest([]any{map[string]any{"id": "owned-triage-ui", "name": "受入: Triage所有モデルHTTP", "endpoint": model.URL + "/v1", "key_file": key, "workspaces": []string{"mcp-one"}, "models": []serverllm.Model{{Requested: "owned-triage-ui-model", Actual: []string{"owned-triage-ui-model"}}}}}), 0600); err != nil {
		t.Fatal(err)
	}
	s.Automation.Gateway = &serverllm.Gateway{ConfigFile: config}
	connections, err := s.Automation.Gateway.Connections("mcp-one")
	if err != nil {
		t.Fatal(err)
	}
	m := func(version string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: "triage-ui-" + time.Now().Format("150405.000000000"), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) access.VersionRef {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatal(v, err)
		}
		return access.VersionRef{ID: v.Data.ID, Version: v.Data.Version}
	}
	put := func(id string, value any) access.VersionRef {
		t.Helper()
		return must(s.Automation.Put(ctx, p, "mcp-one", id, automation.Write{Mutation: m("0"), Value: encodeTest(value)}))
	}
	must(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "triage-ui-project"}, contexts.Write{Mutation: m("0"), Value: encodeTest(contexts.Scope{Kind: "milestone", Name: "Triage画面の受入"})}))
	spec := automation.DefaultModel()
	spec.Connection = connections[0].ID
	spec.Revision = connections[0].Revision
	spec.Model = "owned-triage-ui-model"
	spec.Allowed = []string{spec.Model}
	spec.Limits.Seconds = 3
	price := must(s.Usage.Put(ctx, p, "mcp-one", "price:triage-ui", usage.Write{Mutation: m("0"), Value: encodeTest(usage.Price{Connection: spec.Connection, Model: spec.Model, Currency: "USD", From: time.Now().Add(-time.Hour), Source: "owned fictional UI tariff", Scale: 4, Rounding: "half_up", Rates: map[string]usage.Rate{"input": {Amount: "0.001", Per: "1", Unit: "token"}, "output": {Amount: "0.001", Per: "1", Unit: "token"}, "cache_read": {Amount: "0", Per: "1", Unit: "token"}, "reasoning": {Amount: "0", Per: "1", Unit: "token"}}})}))
	spec.Prices = map[string]usage.Ref{spec.Model: {ID: price.ID, Version: price.Version}}
	policy := put("policy:triage-ui", automation.Policy{Name: "受入: 合成障害の優先度", Scopes: []string{"triage-ui-project"}, Levels: automation.DefaultLevels(), Criteria: "合成障害による利用者への影響を評価する。工数・人数の欠測は不明のままにする。", Context: []access.VersionRef{}, Model: &spec})
	put("activation:triage-ui", automation.Activation{Policy: policy, Enabled: true, AutoApply: true})
	control, err := s.Automation.Read(ctx, p, "mcp-one", access.VersionRef{ID: "control:triage", Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	must(s.Automation.Put(ctx, p, "mcp-one", "control:triage", automation.Write{Mutation: m(control.Version), Value: encodeTest(automation.Control{Kind: "triage", Enabled: true, CanRead: true, CanCommit: true, Slots: 2, MaxAttempts: 2, LeaseSeconds: 30, TimeoutSeconds: 10, MaxRequests: 1000, PeriodSeconds: 3600})}))
	for _, id := range []string{"triage-ui-outage", "triage-ui-followup"} {
		scope := must(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: id}, contexts.Write{Mutation: m("0"), Value: encodeTest(contexts.Scope{Kind: "issue", Name: "受入: 合成障害", Parent: &contexts.Identity{Kind: "milestone", ID: "triage-ui-project"}})}))
		put("issue:"+id, automation.Issue{Scope: scope, Origin: "human", Title: "受入: 合成障害の優先度", Body: "合成障害で利用者の作業が止まっています。実サービスの情報は含みません。", State: "open", Dependencies: []access.VersionRef{}})
	}
	s.Automation.Start(ctx)
}
