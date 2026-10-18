package publications

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPriorityRemoteUsesOnlyIssueLabels(t *testing.T) {
	writes := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path != "/app" && r.Header.Get("Authorization") != "Bearer owned-installation") || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Error("installation credential/version")
		}
		switch r.URL.Path {
		case "/app":
			if strings.Count(r.Header.Get("Authorization"), ".") != 2 {
				t.Error("App identity requires signed JWT")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 123, "slug": "owned"})
		case "/users/owned[bot]":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 55, "login": "owned[bot]", "type": "Bot"})
		case "/repos/owner/repo/issues/7/events":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": 23, "event": "labeled", "label": map[string]string{"name": "priority:high"}, "performed_via_github_app": map[string]int{"id": 123}},
				map[string]any{"id": 24, "event": "labeled", "label": map[string]string{"name": "priority:high"}, "actor": map[string]any{"id": 55, "type": "Bot"}},
				map[string]any{"id": 25, "event": "unlabeled", "actor": map[string]any{"id": 999, "type": "User"}},
				map[string]any{"id": 26, "event": "labeled", "actor": map[string]any{"id": 55, "type": "Bot"}, "performed_via_github_app": map[string]int{"id": 456}},
			})
		case "/repos/owner/repo/issues/7":
			if r.Method != "GET" {
				t.Error("Issue body/state mutated")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "number": 7, "html_url": "https://github.com/owner/repo/issues/7", "title": "owned", "body": "unchanged", "state": "open", "updated_at": time.Now(), "labels": []any{map[string]string{"name": "priority:high"}, map[string]string{"name": "unrelated"}}})
		case "/repos/owner/repo/issues/7/labels":
			writes++
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["labels"] == nil {
				t.Error("write exceeded labels")
			}
			w.WriteHeader(200)
		case "/repos/owner/repo/issues/7/labels/priority:low":
			writes++
			if r.Method != "DELETE" {
				t.Error(r.Method)
			}
			w.WriteHeader(200)
		default:
			t.Error("unexpected external path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer backend.Close()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	remote := NewRemote(&Config{AppID: "123", key: key, Connections: []Connection{{ID: "owned", Workspace: "one", Repository: "owner/repo", RepositoryID: 1, InstallationID: 2}}})
	remote.base = backend.URL
	remote.tokens["one:owned"] = githubapp.Token{Value: "owned-installation", Expires: time.Now().Add(time.Hour)}
	snapshot, err := remote.ReadPriority(context.Background(), "one", "owned", 7, 1)
	if err != nil || len(snapshot.Labels) != 2 || len(snapshot.Events) != 4 || !snapshot.Events[0].Own || !snapshot.Events[1].Own || snapshot.Events[2].Own || snapshot.Events[3].Own {
		t.Fatal(snapshot, err)
	}
	if err = remote.WritePriority(context.Background(), "one", "owned", 7, "labeled", "priority:high"); err != nil {
		t.Fatal(err)
	}
	if err = remote.WritePriority(context.Background(), "one", "owned", 7, "unlabeled", "priority:low"); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.ReadPriority(context.Background(), "two", "owned", 7, 1); err == nil {
		t.Fatal("foreign repository connection")
	}
	if writes != 2 {
		t.Fatal(writes)
	}
	remote.botID = 0
	remote.config.AppID = "456"
	if _, err = remote.ReadPriority(context.Background(), "one", "owned", 7, 1); err == nil {
		t.Fatal("accepted bot from another authenticated App")
	}
}
