package publications

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPriorityRemoteUsesOnlyIssueLabels(t *testing.T) {
	writes := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer owned-installation" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Error("installation credential/version")
		}
		switch r.URL.Path {
		case "/repos/owner/repo/issues/7/events":
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 23, "event": "labeled", "label": map[string]string{"name": "priority:high"}, "performed_via_github_app": map[string]int{"id": 123}}})
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
	remote := NewRemote(&Config{AppID: "123", Connections: []Connection{{ID: "owned", Workspace: "one", Repository: "owner/repo", RepositoryID: 1, InstallationID: 2}}})
	remote.base = backend.URL
	remote.tokens["one:owned"] = githubapp.Token{Value: "owned-installation", Expires: time.Now().Add(time.Hour)}
	snapshot, err := remote.ReadPriority(context.Background(), "one", "owned", 7, 1)
	if err != nil || len(snapshot.Labels) != 2 || len(snapshot.Events) != 1 || !snapshot.Events[0].Own {
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
}
