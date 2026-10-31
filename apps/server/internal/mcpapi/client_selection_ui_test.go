package mcpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

type clientSelectionUI struct {
	Handler        http.Handler
	generationEmit chan chan clientops.GenerationSource
	emitGeneration func() clientops.GenerationSource
	done           chan struct{}
	mu             sync.Mutex
	observed       []map[string]any
	violation      bool
	site           string
	modelSite      string
	expected       []contexts.Ref
}

func (f *clientSelectionUI) Wait(t *testing.T, s *Service, ctx context.Context) {
	t.Helper()
	t.Logf("Client selection UI: %s/__fixture/browser-login ; cross-Origin HTTPS owned provider: %s/v1 ; model names owned-alpha / owned-beta; inert key owned-model-key", f.site, f.modelSite)
	if os.Getenv("KOUJOU_TEST_OUTPUT_CONTEXT_UI") == "1" {
		emitted := false
	waiting:
		for {
			select {
			case <-f.done:
				break waiting
			case response := <-f.generationEmit:
				if emitted {
					response <- clientops.GenerationSource{}
				} else {
					response <- f.emitGeneration()
					emitted = true
				}
			case <-time.After(30 * time.Minute):
				t.Fatal("context generation UI fixture timed out")
			}
		}
	} else {
		select {
		case <-f.done:
		case <-time.After(30 * time.Minute):
			t.Fatal("client selection UI fixture timed out")
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.violation {
		t.Fatal("owned model receiver detected prohibited credentials or data")
	}
	if os.Getenv("KOUJOU_TEST_OUTPUT_CONTEXT_UI") == "1" {
		contextGenerationUIEvidence(t, s, ctx, f.site, f.observed)
		return
	}
	if os.Getenv("KOUJOU_TEST_USAGE_COMPARISON_UI") == "1" {
		usageComparisonUIEvidence(t, s, ctx, f.site)
		return
	}
	if os.Getenv("KOUJOU_TEST_DEFINITION_UI") == "1" {
		definitionUIEvidence(t, s, ctx, f.site, f.observed)
		return
	}
	rows, err := s.ClientOps.Store.Database().Query(ctx, `SELECT jsonb_build_object('id',c.id,'version',c.version,'mode',c.payload#>>'{input,mode}','roots',jsonb_array_length(COALESCE(c.payload#>'{input,include}','[]'::jsonb)),'placements',jsonb_array_length(COALESCE(r.payload->'placements','[]'::jsonb)),'run_id',r.payload->>'run_id') FROM client_operation_records c JOIN context_records r ON r.workspace_id=c.workspace_id AND r.resource_id='manifest:'||(c.payload#>>'{manifest,id}') AND r.revision=c.payload#>>'{manifest,revision}' WHERE c.workspace_id='mcp-one' AND c.kind='selection' AND c.payload->>'name' LIKE '受入%' ORDER BY c.position LIMIT 100`)
	if err != nil {
		t.Fatal(err)
	}
	saved := []json.RawMessage{}
	for rows.Next() {
		var v json.RawMessage
		if err = rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		saved = append(saved, v)
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	b, _ := json.MarshalIndent(map[string]any{"site": f.site, "model_site": f.modelSite, "expected": f.expected, "requests": f.observed, "saved": saved}, "", "  ")
	_ = os.WriteFile("../../../../test-results/issue26-client-ui.json", b, 0600)
}
func newClientSelectionUI(t *testing.T, ctx context.Context, s *Service, client *http.Client, cfg auth.Config, p access.Principal) *clientSelectionUI {
	t.Helper()
	f := &clientSelectionUI{done: make(chan struct{}, 1), site: cfg.Site}
	certificate, err := tls.LoadX509KeyPair("../../../../.cache/tls/cert.pem", "../../../../.cache/tls/key.pem")
	if err != nil {
		t.Fatal(err)
	}
	modelServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == f.site {
			w.Header().Set("Access-Control-Allow-Origin", f.site)
			w.Header().Set("Access-Control-Allow-Headers", "authorization,content-type")
			w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		f.model(w, r, s, ctx)
	}))
	modelServer.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	modelServer.StartTLS()
	t.Cleanup(modelServer.Close)
	f.modelSite = strings.Replace(modelServer.URL, "127.0.0.1", "localhost", 1)
	if os.Getenv("KOUJOU_TEST_OUTPUT_CONTEXT_UI") == "1" {
		seedContextGenerationUI(t, ctx, s, p, cfg, f)
	}
	if os.Getenv("KOUJOU_TEST_USAGE_COMPARISON_UI") == "1" {
		seedUsageComparisonUI(t, ctx, s, p, cfg)
	}
	serial := 0
	put := func(kind, id string, children []contexts.Ref) contexts.Ref {
		t.Helper()
		serial++
		text := "選択受入: 固定した規約と参照の選択を確認する所有データ。"
		unit := contexts.Unit{Kind: kind, Name: "選択受入 " + id, Children: children}
		if kind == "sentence" {
			unit.Text = &text
		}
		if kind == "section" || kind == "document" {
			unit.Title = unit.Name
		}
		b, _ := json.Marshal(unit)
		r, err := s.Contexts.Put(ctx, p, "mcp-one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: apicontract.Mutation{OperationID: fmt.Sprint("selection-ui-", serial), ExpectedVersion: "0"}, Value: b})
		if err != nil || r.Error != nil || r.Data == nil {
			t.Fatal("client UI Context fixture", err)
		}
		return contexts.Ref{Kind: kind, ID: id, Revision: r.Data.Version}
	}
	for i, level := range []string{"sentence", "paragraph", "section", "document"} {
		child := put("sentence", fmt.Sprintf("selection-ui-%d-s", i), nil)
		if level != "sentence" {
			child = put("paragraph", fmt.Sprintf("selection-ui-%d-p", i), []contexts.Ref{child})
		}
		if level == "section" || level == "document" {
			child = put("section", fmt.Sprintf("selection-ui-%d-c", i), []contexts.Ref{child})
		}
		if level == "document" {
			child = put("document", fmt.Sprintf("selection-ui-%d-d", i), []contexts.Ref{child})
		}
		f.expected = append(f.expected, child)
	}
	if err := s.Search.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Graph.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := driveOwnedAuthorization(ctx, client, cfg, cfg.Site+"/auth/start", false); err != nil {
		t.Fatal("owned browser API login", err)
	}
	root, _ := url.Parse(cfg.Site)
	cookies := client.Jar.Cookies(root)
	files := http.FileServer(http.Dir("../../../client/dist"))
	f.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__fixture/browser-login":
			for _, c := range cookies {
				if c.Name == "__Host-koujou-session" || c.Name == "__Host-koujou-csrf" || strings.Contains(c.Name, "kratos") {
					http.SetCookie(w, &http.Cookie{Name: c.Name, Value: c.Value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
				}
			}
			http.Redirect(w, r, "/", 303)
			return
		case "/__fixture/finish":
			select {
			case f.done <- struct{}{}:
			default:
			}
			w.WriteHeader(204)
			return
		case "/__fixture/generation-source":
			if r.Method != "POST" || f.generationEmit == nil {
				http.Error(w, "owned source unavailable", 400)
				return
			}
			response := make(chan clientops.GenerationSource, 1)
			f.generationEmit <- response
			json.NewEncoder(w).Encode(<-response)
			return
		case "/__fixture/native-authorize":
			var v struct {
				URL string `json:"url"`
			}
			if json.NewDecoder(r.Body).Decode(&v) != nil {
				http.Error(w, "bad owned authorize", 400)
				return
			}
			u, err := url.Parse(v.URL)
			if err != nil || !strings.HasPrefix(v.URL, cfg.Issuer+"oauth2/auth?") || u.Query().Get("client_id") != cfg.NativeClient || u.Query().Get("redirect_uri") != cfg.NativeRedirect {
				http.Error(w, "owned client only", 403)
				return
			}
			if err = driveOwnedAuthorization(ctx, client, cfg, v.URL, true); err != nil {
				http.Error(w, "owned authorization failed", 500)
				return
			}
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, "../../../client/dist/index.html")
	})
	return f
}
func driveOwnedAuthorization(ctx context.Context, client *http.Client, cfg auth.Config, next string, native bool) error {
	for range 24 {
		u, err := url.Parse(next)
		if err != nil {
			return err
		}
		if u.Path == "/auth/login" || u.Path == "/auth/consent" {
			resp, err := client.Get(cfg.Site + "/auth/csrf")
			if err != nil {
				return err
			}
			var csrf map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&csrf)
			_ = resp.Body.Close()
			kind := strings.TrimPrefix(u.Path, "/auth/")
			request, _ := http.NewRequestWithContext(ctx, "POST", cfg.Site+"/auth/broker/"+kind+"?challenge="+url.QueryEscape(u.Query().Get(kind+"_challenge")), nil)
			request.Header.Set("Origin", cfg.Site)
			request.Header.Set("X-CSRF-Token", csrf["csrf"])
			resp, err = client.Do(request)
			if err != nil {
				return err
			}
			var value map[string]string
			_ = json.NewDecoder(resp.Body).Decode(&value)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || value["redirect"] == "" {
				return fmt.Errorf("owned broker %s status %d", kind, resp.StatusCode)
			}
			next = value["redirect"]
			continue
		}
		resp, err := client.Get(next)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if (!native && (u.Path == "/auth/callback" || u.Path == "/auth/mcp/callback")) || (native && strings.HasPrefix(next, cfg.NativeRedirect+"?")) {
			if resp.StatusCode >= 400 {
				return fmt.Errorf("owned callback status %d", resp.StatusCode)
			}
			return nil
		}
		next = resp.Header.Get("Location")
		if next == "" {
			return fmt.Errorf("owned authorization status %d", resp.StatusCode)
		}
	}
	return fmt.Errorf("owned authorization did not finish")
}
func (f *clientSelectionUI) model(w http.ResponseWriter, r *http.Request, s *Service, ctx context.Context) {
	var body struct {
		Format struct {
			Schema struct {
				Name string `json:"name"`
			} `json:"json_schema"`
		} `json:"response_format"`
		Model    string `json:"model"`
		Messages []struct {
			Role, Content string
			CallID        string `json:"tool_call_id"`
		} `json:"messages"`
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil || json.Unmarshal(raw, &body) != nil {
		http.Error(w, "invalid owned model request", 400)
		return
	}
	bad := r.Header.Get("Authorization") != "Bearer owned-model-key" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Koujou-Operation") != "" || bytes.Contains(raw, []byte("FORBIDDEN_IMPL_DIALOGUE")) || bytes.Contains(raw, []byte("FORBIDDEN_SELF_EVALUATION")) || bytes.Contains(raw, []byte("FORBIDDEN_OTHER_INPUT")) || bytes.Contains(raw, []byte("FORBIDDEN_OTHER_CHANNEL_DATA"))
	f.mu.Lock()
	f.violation = f.violation || bad
	f.observed = append(f.observed, map[string]any{"model": body.Model, "messages": len(body.Messages), "cookie_absent": r.Header.Get("Cookie") == "", "mcp_header_absent": r.Header.Get("X-Koujou-Operation") == ""})
	f.mu.Unlock()
	if bad {
		http.Error(w, "owned receiver boundary failed", 400)
		return
	}
	if body.Format.Schema.Name == "output_context" {
		_ = s.Search.Sync(ctx)
		_ = s.Graph.Sync(ctx)
		contextGenerationUIModel(w, raw)
		return
	}
	if body.Format.Schema.Name == "run_definition" {
		definitionUIModel(w, raw)
		return
	}
	_ = s.Search.Sync(ctx)
	_ = s.Graph.Sync(ctx)
	searchDone := false
	cursor := ""
	got := map[string]bool{}
	for _, msg := range body.Messages {
		if msg.Role != "tool" {
			continue
		}
		if strings.HasPrefix(msg.CallID, "get-") {
			got[msg.CallID] = true
		}
		if strings.HasPrefix(msg.CallID, "search") {
			var tool struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			_ = json.Unmarshal([]byte(msg.Content), &tool)
			if len(tool.Content) > 0 {
				var page struct {
					Meta apicontract.Meta `json:"meta"`
				}
				_ = json.Unmarshal([]byte(tool.Content[0].Text), &page)
				searchDone = true
				cursor = ""
				if page.Meta.NextCursor != nil {
					cursor = *page.Meta.NextCursor
				}
			}
		}
	}
	message := map[string]any{"role": "assistant"}
	finish := "stop"
	tool := func(id, name string, args any) {
		b, _ := json.Marshal(args)
		message["tool_calls"] = []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(b)}}}
		message["content"] = nil
		finish = "tool_calls"
	}
	if !searchDone || cursor != "" {
		tool("search-"+rand.Text(), "context_search", map[string]any{"text": "選択受入", "limit": 2, "cursor": cursor})
	} else {
		missing := -1
		for i := range f.expected {
			if !got[fmt.Sprintf("get-%d", i)] {
				missing = i
				break
			}
		}
		if missing >= 0 {
			tool(fmt.Sprintf("get-%d", missing), "context_get", f.expected[missing])
		} else {
			items := []any{}
			selected := f.expected
			if body.Model == "owned-beta" {
				selected = selected[:2]
			}
			for _, ref := range selected {
				items = append(items, map[string]any{"ref": ref, "reason": "所有する受入規約の固定版"})
			}
			b, _ := json.Marshal(map[string]any{"items": items, "questions": []string{}, "unknown": []string{}})
			message["content"] = string(b)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-" + rand.Text(), "model": body.Model, "choices": []any{map[string]any{"message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "prompt_tokens_details": map[string]any{"cached_tokens": 0}, "completion_tokens_details": map[string]any{"reasoning_tokens": 0}}})
}
