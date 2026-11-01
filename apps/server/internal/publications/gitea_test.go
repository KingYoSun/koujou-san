package publications

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGiteaPublicationAndLostResponse(t *testing.T) {
	var current Issue
	posts, lose := 0, false
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token owned-publisher" {
			t.Error("wrong instance credential")
		}
		switch {
		case r.URL.Path == "/api/v1/repos/proof/private":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"proof/private"}`))
		case r.URL.Path == "/api/v1/repos/proof/private/issues" && r.Method == "POST":
			var input struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			posts++
			current = Issue{ID: 7, Number: 7, URL: server.URL + "/proof/private/issues/7", Title: input.Title, Body: input.Body, State: "open", UpdatedAt: time.Now().UTC(), PullRequest: json.RawMessage("null")}
			if lose {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(current)
		case r.URL.Path == "/api/v1/repos/proof/private/issues/7":
			_ = json.NewEncoder(w).Encode(current)
		case r.URL.Path == "/api/v1/repos/proof/private/issues" && r.Method == "GET":
			if r.URL.Query().Get("type") != "issues" || r.URL.Query().Get("q") == "" {
				t.Error("unscoped issue lookup")
			}
			_ = json.NewEncoder(w).Encode([]Issue{current})
		default:
			t.Errorf("unexpected Gitea operation %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	token, ca := filepath.Join(dir, "token"), filepath.Join(dir, "ca")
	_ = os.WriteFile(token, []byte("owned-publisher"), 0600)
	_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	data, _ := json.Marshal(map[string]any{"gitea_instances": []any{map[string]any{"url": server.URL, "token_file": token, "ca_file": ca}}, "connections": []any{map[string]any{"id": "proof", "workspace_id": "one", "provider": "gitea", "instance": server.URL, "repository": "proof/private", "repository_id": 1, "actor_id": 1}}})
	file := filepath.Join(dir, "config.json")
	_ = os.WriteFile(file, data, 0600)
	t.Setenv("KOUJOU_PUBLICATION_CONFIG", file)
	config, err := Environment()
	if err != nil {
		t.Fatal(err)
	}
	remote := NewRemote(config)
	connection, _ := remote.Connection("one", "proof")
	ctx := context.Background()
	tag := marker("one", "gitea-first")
	created := remote.Create(ctx, connection, Draft{Title: "owned", Body: "proof"}, "proof\n"+tag)
	if created.State != "created" || posts != 1 {
		t.Fatal("Gitea create was not fetched", created.State, posts)
	}
	lose = true
	tag = marker("one", "gitea-lost")
	lost := remote.Create(ctx, connection, Draft{Title: "lost"}, "proof\n"+tag)
	if lost.State != "unknown" || posts != 2 {
		t.Fatal("lost response claimed success", lost.State, posts)
	}
	observed := remote.Observe(ctx, connection, tag, 0)
	if observed.State != "created" || observed.Issue == nil || observed.Issue.Number != 7 || posts != 2 {
		t.Fatal("unknown outcome was re-created", observed.State, posts)
	}
	changed := connection
	changed.RepositoryID = 2
	if remote.Create(ctx, changed, Draft{Title: "wrong"}, "wrong").State != "failed" || posts != 2 {
		t.Fatal("changed repository ID wrote externally")
	}
}

func TestGiteaPublicationLive(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_GITEA_LIVE") != "1" {
		t.Skip("explicit self-hosted Gitea publication test only")
	}
	c, err := Environment()
	if err != nil || c == nil {
		t.Fatal("configure KOUJOU_PUBLICATION_CONFIG")
	}
	remote := NewRemote(c)
	output := os.Getenv("KOUJOU_GITEA_EVIDENCE")
	if output == "" {
		t.Fatal("configure a durable live request/evidence file")
	}
	requests := map[string]string{}
	if b, err := os.ReadFile(output + ".requests.json"); err == nil {
		if json.Unmarshal(b, &requests) != nil {
			t.Fatal("invalid saved live requests")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	results := []Outcome{}
	for _, connection := range c.Connections {
		if connection.Provider != "gitea" {
			continue
		}
		key := connection.ID + "@" + connection.Instance + "/" + connection.Repository
		tag, existing := requests[key]
		if !existing {
			tag = marker(connection.Workspace, "gitea-live-"+rand.Text())
			requests[key] = tag
			b, _ := json.MarshalIndent(requests, "", "  ")
			if err := os.WriteFile(output+".requests.json", b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		draft := Draft{Title: "Issue #83 Gitea publication proof", Body: "専用環境の起票・照合試験。"}
		var created Outcome
		if existing {
			created = remote.Observe(context.Background(), connection, tag, 0)
		} else {
			created = remote.Create(context.Background(), connection, draft, draft.Body+"\n"+tag)
		}
		if created.State != "created" || created.Issue == nil {
			t.Fatalf("%s: %s %s", connection.ID, created.State, created.Reason)
		}
		observed := remote.Observe(context.Background(), connection, tag, 0)
		for i := 0; i < 30 && observed.State == "unknown"; i++ {
			time.Sleep(time.Second)
			observed = remote.Observe(context.Background(), connection, tag, 0)
		}
		if observed.State != "created" || observed.Issue == nil || created.Issue.ID != observed.Issue.ID {
			t.Fatalf("%s marker reconciliation failed: %s", connection.ID, observed.Reason)
		}
		results = append(results, observed)
	}
	if id := os.Getenv("KOUJOU_GITEA_PRIORITY_CONNECTION"); id != "" {
		connection, ok := remote.Connection("mcp-one", id)
		if !ok {
			t.Fatal("configure the dedicated priority connection")
		}
		ctx := context.Background()
		label := "issue83-priority-proof"
		if err := remote.WritePriority(ctx, connection.Workspace, connection.ID, 1, "unlabeled", label); err != nil {
			t.Fatal("Gitea priority removal", err)
		}
		removed, err := remote.ReadPriority(ctx, connection.Workspace, connection.ID, 1, 1)
		if err != nil || len(removed.Events) == 0 || removed.Events[len(removed.Events)-1].Action != "unlabeled" || !removed.Events[len(removed.Events)-1].Own {
			t.Fatal("Gitea removal timeline", err)
		}
		if err := remote.WritePriority(ctx, connection.Workspace, connection.ID, 1, "labeled", label); err != nil {
			t.Fatal("Gitea priority addition", err)
		}
		added, err := remote.ReadPriority(ctx, connection.Workspace, connection.ID, 1, 1)
		if err != nil || len(added.Labels) != 1 || added.Labels[0] != label {
			t.Fatal("Gitea label did not converge", err)
		}
	}
	{
		data, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("created and independently fetched %d Gitea issues using scoped publication credentials", len(results))
}
