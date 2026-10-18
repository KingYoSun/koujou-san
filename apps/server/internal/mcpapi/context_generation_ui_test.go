package mcpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func seedContextGenerationUI(t *testing.T, ctx context.Context, s *Service, p access.Principal, cfg auth.Config, f *clientSelectionUI) {
	t.Helper()
	text := "生成受入の共有記述。固定出力から観測を再利用する。"
	v, err := s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "sentence", ID: "generation-ui-shared"}, contexts.Write{Mutation: mutationTest("generation-ui-shared"), Value: encodeTest(contexts.Unit{Kind: "sentence", Name: "生成受入 共有記述", Text: &text})})
	if err != nil || v.Data == nil {
		t.Fatal(v.Error, err)
	}
	f.generationEmit = make(chan chan clientops.GenerationSource)
	f.emitGeneration = func() clientops.GenerationSource {
		source, _ := generationSourceFixture(t, ctx, s, p, cfg.Key, "context-generation-auto-ui", contexts.Ref{})
		return source
	}
}
func mutationTest(id string) apicontract.Mutation {
	return apicontract.Mutation{OperationID: id, ExpectedVersion: "0"}
}

func contextGenerationUIModel(w http.ResponseWriter, raw []byte) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role, Content string
			CallID        string `json:"tool_call_id"`
		} `json:"messages"`
	}
	json.Unmarshal(raw, &req)
	var input struct {
		Mode       string `json:"mode"`
		Generation struct {
			Source clientops.GenerationSource `json:"source"`
		} `json:"generation"`
	}
	for _, m := range req.Messages {
		if m.Role == "user" && strings.Contains(m.Content, `"purpose":"context-generation"`) {
			json.Unmarshal([]byte(m.Content), &input)
			break
		}
	}
	gotSource, gotSearch, gotContext := false, false, false
	var existing contexts.Ref
	var groups []struct {
		Ref  contexts.Ref `json:"ref"`
		Kind string       `json:"kind"`
	}
	for _, m := range req.Messages {
		if m.Role != "tool" {
			continue
		}
		var wire struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		json.Unmarshal([]byte(m.Content), &wire)
		if len(wire.Content) == 0 {
			continue
		}
		body := []byte(wire.Content[0].Text)
		if m.CallID == "generation-source" {
			gotSource = true
			var data struct {
				Data struct {
					GroupScopes []struct {
						Ref  contexts.Ref `json:"ref"`
						Kind string       `json:"kind"`
					} `json:"group_scopes"`
				} `json:"data"`
			}
			json.Unmarshal(body, &data)
			groups = data.Data.GroupScopes
		}
		if m.CallID == "generation-search" {
			gotSearch = true
			var data struct {
				Data struct {
					Items []struct {
						Ref contexts.Ref `json:"ref"`
					} `json:"items"`
				} `json:"data"`
			}
			json.Unmarshal(body, &data)
			for _, item := range data.Data.Items {
				if item.Ref.ID == "generation-ui-shared" {
					existing = item.Ref
					break
				}
			}
		}
		if m.CallID == "generation-existing" {
			gotContext = true
		}
	}
	message := map[string]any{"role": "assistant"}
	finish := "stop"
	tool := func(id, name string, args any) {
		message["content"] = nil
		message["tool_calls"] = []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": mustJSON(args)}}}
		finish = "tool_calls"
	}
	if !gotSource {
		tool("generation-source", "run_get", input.Generation.Source.Output)
	} else if input.Mode != "none" && !gotSearch {
		tool("generation-search", "context_search", map[string]any{"text": "生成受入 共有記述", "limit": 10})
	} else if existing.ID != "" && !gotContext {
		tool("generation-existing", "context_get", existing)
	} else {
		target := contexts.Ref{Kind: "sentence", ID: "$fact", Revision: "0"}
		if existing.ID != "" {
			target = existing
		}
		text := "固定出力では、invalid_countを期待した検査がNaNを返した。"
		if req.Model == "owned-beta" {
			text = "固定出力のNaNは観測であり、入力検証の不足という説明は推測として扱う。"
		}
		changes := clientops.GenerationProposal{Changes: []clientops.GenerationChange{{Key: "fact", Target: target, ValueUTF8: mustJSON(map[string]any{"kind": "sentence", "name": "生成受入 共有記述", "text": text}), State: "observed", Reason: "固定したchecks/invalidを参照", References: []contexts.Ref{}}}, Grouping: []clientops.GenerationGrouping{}, Candidates: []clientops.GenerationCandidate{}}
		if existing.ID != "" {
			changes.Candidates = append(changes.Candidates, clientops.GenerationCandidate{Ref: existing, Relation: "reuse", Reason: "同じ明示した既存版を更新"})
		}
		for i, kind := range []string{"paragraph", "section", "document"} {
			child := []string{"fact", "paragraph", "section"}[i]
			childKind := []string{"sentence", "paragraph", "section"}[i]
			value := map[string]any{"kind": kind, "name": "生成受入 " + kind, "children": []contexts.Ref{{Kind: childKind, ID: "$" + child, Revision: "new"}}}
			if kind != "paragraph" {
				value["title"] = "生成受入 " + kind
			}
			changes.Changes = append(changes.Changes, clientops.GenerationChange{Key: kind, Target: contexts.Ref{Kind: kind, ID: "$" + kind, Revision: "0"}, ValueUTF8: mustJSON(value), State: "proposal", Reason: "本文を複製せず順序付き参照で構成", References: []contexts.Ref{}})
		}
		if len(groups) > 0 {
			changes.Grouping = append(changes.Grouping, clientops.GenerationGrouping{Key: "group", Scope: contexts.Identity{Kind: groups[0].Kind, ID: groups[0].Ref.ID}, Context: contexts.Identity{Kind: "document", ID: "$document"}, Present: true, ExpectedVersion: "0"})
		}
		message["content"] = mustJSON(map[string]any{"changes_utf8": mustJSON(changes), "reason": "固定した原出力を保持した由来付き変更案", "questions": []string{}, "unknown": []string{}})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": "owned-context-" + rand.Text(), "model": req.Model, "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 60}})
}
func contextGenerationUIEvidence(t *testing.T, s *Service, ctx context.Context, site string, requests []map[string]any) {
	t.Helper()
	var summary []byte
	err := s.Access.DB.QueryRow(ctx, `SELECT jsonb_build_object('operations',(SELECT jsonb_agg(jsonb_build_object('id',r.id,'state',r.payload->>'state','model',r.payload#>>'{model,connection}','target',r.payload->'target','applied',r.payload->'applied')) FROM client_operation_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.kind='operation' AND r.payload->>'purpose'='context-generation'),'origins',(SELECT jsonb_agg(payload) FROM client_context_origins),'queues',(SELECT count(*) FROM context_generation_queue),'source_outputs',(SELECT count(*) FROM run_records WHERE kind='output' AND run_id LIKE 'run:context-generation%'))`).Scan(&summary)
	if err != nil {
		t.Fatal(err)
	}
	var data any
	json.Unmarshal(summary, &data)
	b, _ := json.MarshalIndent(map[string]any{"site": site, "requests": requests, "summary": data}, "", "  ")
	if err = os.WriteFile("../../../../test-results/issue31-context-ui.json", b, 0600); err != nil {
		t.Fatal(err)
	}
}
