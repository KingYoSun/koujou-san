package mcpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
)

// Owned model HTTP exercises the production Host, MCP reads and definition writes.
// It never calls a provider or starts a local CLI.
func definitionUIModel(w http.ResponseWriter, raw []byte) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role, Content string
			CallID        string `json:"tool_call_id"`
		} `json:"messages"`
	}
	json.Unmarshal(raw, &req)
	var input struct {
		Goal       string `json:"goal"`
		Mode       string `json:"mode"`
		Definition struct {
			Sources    []clientops.Ref `json:"sources"`
			FixedPaths []string        `json:"fixed_paths"`
		} `json:"definition"`
	}
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, `"purpose":"definition"`) {
			json.Unmarshal([]byte(m.Content), &input)
			break
		}
	}
	runner := clientops.Ref{}
	for _, r := range input.Definition.Sources {
		if strings.HasPrefix(r.ID, "runner:") {
			runner = r
			break
		}
	}
	checked := false
	for _, m := range req.Messages {
		if m.Role == "tool" && m.CallID == "definition-capabilities" {
			checked = true
		}
	}
	message := map[string]any{"role": "assistant"}
	finish := "stop"
	if runner.ID != "" && !checked {
		message["content"] = nil
		message["tool_calls"] = []any{map[string]any{"id": "definition-capabilities", "type": "function", "function": map[string]any{"name": "workflow_capabilities", "arguments": mustJSON(map[string]string{"device": strings.TrimPrefix(runner.ID, "runner:"), "version": runner.Version})}}}
		finish = "tool_calls"
	} else {
		questions := []string{}
		definition := map[string]any{"schema": "run-definition-v1", "name": "受入: 生成したRun定義"}
		if runner.ID == "" {
			questions = append(questions, "利用する端末と接続を指定してください。作業場所は接続設定とRun専用worktreeを使いますか？")
		} else {
			target := "codex"
			if req.Model == "owned-beta" {
				target = "claude"
			}
			definition["device_id"] = strings.TrimPrefix(runner.ID, "runner:")
			definition["selection"] = map[string]any{"mode": "none"}
			definition["spec"] = map[string]any{"name": "合成の受入Run", "input": map[string]any{"prompt": "合成の入力のみ。実行は別操作。", "command": map[string]any{"kind": "cli", "target": target, "parameters": []string{}, "binding": map[string]string{"prompt": "stdin"}}}}
		}
		for _, path := range input.Definition.FixedPaths {
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			node := definition
			for _, p := range parts[:len(parts)-1] {
				node, _ = node[p].(map[string]any)
			}
			if node != nil {
				delete(node, parts[len(parts)-1])
			}
		}
		message["content"] = mustJSON(map[string]any{"definition_utf8": mustJSON(definition), "reason": "依頼と確認済みの論理接続に基づく所有モデルの生成案", "questions": questions, "unknown": []string{}})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": "owned-definition-" + rand.Text(), "model": req.Model, "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 30}})
}
func definitionUIEvidence(t *testing.T, s *Service, ctx context.Context, site string, requests []map[string]any) {
	t.Helper()
	var counts []byte
	err := s.Access.DB.QueryRow(ctx, `SELECT jsonb_build_object('definitions',(SELECT count(*) FROM workflow_records WHERE kind='run-definition'),'operations',(SELECT jsonb_agg(jsonb_build_object('id',c.id,'state',c.payload->>'state','model',c.payload#>>'{model,connection}','target',c.payload->'target','applied',c.payload->'applied')) FROM client_operation_records c JOIN shared_versions v ON v.workspace_id=c.workspace_id AND v.resource_id=c.id AND v.version=c.version WHERE c.kind='operation' AND c.payload->>'purpose'='definition'),'runs',(SELECT count(DISTINCT id) FROM run_records WHERE id LIKE 'run:%'),'pauses',(SELECT count(*) FROM run_records WHERE kind='pause'))`).Scan(&counts)
	if err != nil {
		t.Fatal(err)
	}
	var summary any
	json.Unmarshal(counts, &summary)
	b, _ := json.MarshalIndent(map[string]any{"site": site, "requests": requests, "summary": summary}, "", "  ")
	if err = os.WriteFile("../../../../test-results/issue30-definition-ui.json", b, 0600); err != nil {
		t.Fatal(err)
	}
}
