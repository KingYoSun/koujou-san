package mcpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func seedProjectChatUI(t *testing.T, ctx context.Context, s *Service, p access.Principal) {
	t.Helper()
	accept := func(v apicontract.Response[apicontract.Reference], err error) clientops.Ref {
		t.Helper()
		if err != nil || v.Data == nil || v.Error != nil {
			t.Fatalf("chat UI preparation: %+v %v", v.Error, err)
		}
		return clientops.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	mutation := func(id string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: "chat-ui:" + id, ExpectedVersion: "0"}
	}
	project := accept(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "chat-project"}, contexts.Write{Mutation: mutation("project"), Value: rawUI(contexts.Scope{Kind: "project", Name: "Issue #82の受入Project"})}))
	accept(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "chat-issue83"}, contexts.Write{Mutation: mutation("issue83"), Value: rawUI(contexts.Scope{Kind: "issue", Name: "Issue #83: Gitea連携の準備"})}))
	thread := accept(s.ClientOps.PutChatThread(ctx, p, "mcp-one", "chat-thread:long-history", clientops.Write{Mutation: mutation("thread"), Value: rawUI(clientops.ChatThread{Name: "受入: 長い会話の再訪", Project: project})}))
	for i := range 60 {
		id := fmt.Sprintf("chat-message:history-%02d", i)
		text := fmt.Sprintf("過去の相談 %02d。選択しなければモデルへ送りません。", i)
		accept(s.ClientOps.PutChatMessage(ctx, p, "mcp-one", id, clientops.Write{Mutation: mutation(id), Value: rawUI(clientops.ChatMessage{Thread: thread, Role: "human", Text: text, State: "complete"})}))
	}
}

func projectChatUIEvidence(t *testing.T, s *Service, ctx context.Context, observed []map[string]any) {
	t.Helper()
	rows, err := s.ClientOps.Store.Database().Query(ctx, `SELECT jsonb_build_object('id',r.id,'version',r.version,'kind',r.kind,'root',r.root_id,'value',r.payload) FROM client_operation_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id='mcp-one' AND (r.kind LIKE 'chat-%' OR (r.kind='operation' AND r.payload->>'purpose'='chat')) ORDER BY r.position,r.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var records []json.RawMessage
	for rows.Next() {
		var record json.RawMessage
		if err := rows.Scan(&record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	value, _ := json.MarshalIndent(map[string]any{"records": records, "owned_model_requests": observed}, "", "  ")
	if err := os.WriteFile("../../../../test-results/issue82-chat-ui.json", value, 0600); err != nil {
		t.Fatal(err)
	}
}
