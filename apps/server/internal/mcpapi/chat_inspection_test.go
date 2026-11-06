package mcpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/repositories"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkChatInspectionMCP(t *testing.T, s *Service, p access.Principal, key []byte, connect func(string, string) (*mcp.ClientSession, error), call func(*mcp.ClientSession, string, any, bool) []byte) {
	t.Helper()
	ctx := context.Background()
	serial := 0
	mutation := func() apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("chat-inspect-%d", serial), ExpectedVersion: "0"}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) clientops.Ref {
		t.Helper()
		if err != nil || v.Error != nil || v.Data == nil {
			t.Fatalf("inspection fixture: %+v %v", v.Error, err)
		}
		return clientops.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	commit := strings.Repeat("a", 40)
	file := "# 開発手順\n" + strings.Repeat("本文", 12000)
	requests := 0
	var expectedOffset atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.Header.Get("Authorization") != "token inspection-private-token" {
			t.Errorf("unexpected remote mutation or authentication")
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/api/v1/user":
			json.NewEncoder(w).Encode(map[string]any{"id": 7, "login": "owner"})
		case "/api/v1/repos/owner/project":
			json.NewEncoder(w).Encode(map[string]any{"id": 42, "full_name": "owner/project", "default_branch": "main"})
		case "/api/v1/repos/owner/project/commits/main":
			json.NewEncoder(w).Encode(map[string]string{"sha": commit})
		case "/api/v1/repos/owner/project/git/trees/" + commit:
			if r.URL.Query().Get("per_page") != "50" {
				t.Error("unbounded tree")
			}
			json.NewEncoder(w).Encode(map[string]any{"tree": []any{map[string]any{"path": "README.md", "type": "blob", "sha": strings.Repeat("b", 40), "size": len(file)}}, "truncated": r.URL.Query().Get("page") == "1"})
		case "/api/v1/repos/owner/project/raw/README.md":
			if r.URL.Query().Get("ref") != commit {
				t.Error("file revision not pinned")
			}
			if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-%d", expectedOffset.Load(), expectedOffset.Load()+32768) {
				t.Error("file range does not match requested chunk")
			}
			recorder := httptest.NewRecorder()
			http.ServeContent(recorder, r, "README.md", time.Time{}, strings.NewReader(file))
			if recorder.Body.Len() > 32769 {
				t.Fatal("provider retransmitted the file prefix")
			}
			for name, values := range recorder.Header() {
				w.Header()[name] = values
			}
			w.WriteHeader(recorder.Code)
			w.Write(recorder.Body.Bytes())
		default:
			w.WriteHeader(404)
		}
	}))
	defer remote.Close()
	s.Repositories = repositories.New(s.ClientOps.Store, s.Access, key)
	project := must(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "inspect-project"}, contexts.Write{Mutation: mutation(), Value: json.RawMessage(mustJSON(contexts.Scope{Kind: "project", Name: "Inspection project"}))}))
	other := must(s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: "scope", ID: "inspect-other"}, contexts.Write{Mutation: mutation(), Value: json.RawMessage(mustJSON(contexts.Scope{Kind: "project", Name: "Other project"}))}))
	must(s.Repositories.PutConnection(ctx, p, "mcp-one", "inspect-connection", repositories.ConnectionWrite{Mutation: mutation(), Name: "Owned Gitea", Instance: remote.URL, Token: "inspection-private-token"}))
	for _, v := range []struct{ id, project string }{{"inspect-repository", strings.TrimPrefix(project.ID, "scope:")}, {"inspect-other-repository", strings.TrimPrefix(other.ID, "scope:")}} {
		must(s.Repositories.PutRepository(ctx, p, "mcp-one", v.id, repositories.RepositoryWrite{Mutation: mutation(), Connection: "inspect-connection", Project: v.project, FullName: "owner/project"}, false))
	}
	thread := must(s.ClientOps.PutChatThread(ctx, p, "mcp-one", "chat-thread:inspect", clientops.Write{Mutation: mutation(), Value: json.RawMessage(mustJSON(clientops.ChatThread{Name: "開発開始の確認", Project: project}))}))
	message := must(s.ClientOps.PutChatMessage(ctx, p, "mcp-one", "chat-message:inspect", clientops.Write{Mutation: mutation(), Value: json.RawMessage(mustJSON(clientops.ChatMessage{Thread: thread, Role: "human", Text: "開発準備を確認", State: "complete"}))}))
	for _, enabled := range []bool{false, true} {
		policy := clientops.DefaultPolicy()
		policy.Mode = "proposal"
		policy.Fixed.Mode = "select"
		policy.Scopes = []string{strings.TrimPrefix(project.ID, "scope:")}
		policy.Chat = &clientops.ChatSource{Thread: thread, Messages: []clientops.Ref{message}, Inspect: enabled}
		policyRef := must(s.ClientOps.PutPolicy(ctx, p, "mcp-one", fmt.Sprintf("chat-policy:inspect-%t", enabled), clientops.Write{Mutation: mutation(), Value: json.RawMessage(mustJSON(policy))}))
		opRef := must(s.ClientOps.Start(ctx, p, "mcp-one", fmt.Sprintf("client-operation:inspect-%t", enabled), clientops.Start{Mutation: mutation(), Policy: policyRef, Target: thread, Purpose: "chat", Goal: "開発準備を確認", Client: "inspection-client", Model: clientops.Model{Connection: "owned", Revision: "1", Requested: "owned", Allowed: []string{"owned"}}}))
		record, err := s.ClientOps.Read(ctx, p, "mcp-one", opRef)
		if err != nil {
			t.Fatal(err)
		}
		var op clientops.Operation
		json.Unmarshal(record.Value, &op)
		peer, err := connect(s.Access.OperationHandle("mcp-one", op.ReadOperation, p.UserID), "inspection-client")
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			call(peer, "project_inspect", InspectionRequest{}, true)
			call(peer, "repository_read", repositories.ContentRequest{Repository: "inspect-repository"}, true)
			peer.Close()
			continue
		}
		for _, kind := range []string{"overview", "repositories", "runners", "runs"} {
			data := call(peer, "project_inspect", InspectionRequest{Kind: kind}, false)
			if strings.Contains(string(data), "inspection-private-token") || (kind == "repositories" && strings.Contains(string(data), "inspect-other-repository")) {
				t.Fatal("unrelated or secret data exposed")
			}
		}
		var listing apicontract.Response[repositories.Content]
		json.Unmarshal(call(peer, "repository_read", repositories.ContentRequest{Repository: "inspect-repository"}, false), &listing)
		if listing.Data == nil || listing.Data.Commit != commit || listing.Data.NextPage != 2 || listing.Meta.NextCursor == nil {
			t.Fatalf("tree continuation: %+v", listing)
		}
		var joined string
		var offset int64
		for {
			var chunk apicontract.Response[repositories.Content]
			expectedOffset.Store(offset)
			json.Unmarshal(call(peer, "repository_read", repositories.ContentRequest{Repository: "inspect-repository", Commit: commit, Path: "README.md", Offset: offset}, false), &chunk)
			if chunk.Data == nil || chunk.Data.State != "read" || len(chunk.Data.Text) > 32768 {
				t.Fatalf("file chunk: %+v", chunk)
			}
			joined += chunk.Data.Text
			offset = chunk.Data.NextOffset
			if offset == 0 {
				break
			}
		}
		if joined != file {
			t.Fatal("UTF-8 chunks lost content")
		}
		before := requests
		call(peer, "repository_read", repositories.ContentRequest{Repository: "inspect-other-repository", Path: "README.md"}, true)
		call(peer, "repository_read", repositories.ContentRequest{Repository: "inspect-repository", Path: "../secret"}, true)
		if requests != before {
			t.Fatal("rejected read reached provider")
		}
		old, version, err := s.Access.DeliveryPolicy(ctx, p, "mcp-one")
		if err != nil {
			t.Fatal(err)
		}
		deny := old
		deny.DeniedScopes = append(append([]string{}, old.DeniedScopes...), "inspect-project")
		m := mutation()
		m.ExpectedVersion = version
		denied := must(s.Access.SetDeliveryPolicy(ctx, p, "mcp-one", m, deny))
		call(peer, "project_inspect", InspectionRequest{}, true)
		m = mutation()
		m.ExpectedVersion = denied.Version
		must(s.Access.SetDeliveryPolicy(ctx, p, "mcp-one", m, old))
		peer.Close()
	}
	t.Log("Project inspection: dedicated MCP, current policy, Project-only repositories, fixed commit, bounded tree/file continuation and no writes passed")
}
