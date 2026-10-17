package publications

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only. Uses the configured publisher App; no credential is
// returned. The synthetic issue is edited/closed through the owner's existing
// gh login only to verify canonical observation and clean up that one artifact.
func TestPublicationsLive(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_PUBLICATION_LIVE") != "1" {
		t.Skip("explicit publisher App configuration and isolated database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, err := Environment()
	if err != nil || cfg == nil || len(cfg.Connections) != 1 {
		t.Fatal("one explicit publisher connection required")
	}
	connection := cfg.Connections[0]
	if connection.Repository != "KingYoSun/koujou-san" || connection.RepositoryID != 1418646340 {
		t.Fatal("live fixture repository not approved")
	}
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Fatal("isolated database required")
	}
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w := connection.Workspace
	user := "publication-live-user"
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO auth_users VALUES($1,'fixture',$1,'publication-live@koujou.test','publication live',false)`, []any{user}},
		{`INSERT INTO access_workspaces VALUES($1,'publication live fixture',$2)`, []any{w, user}},
		{`INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES($1,$2,'admin',true)`, []any{w, user}},
	} {
		if _, err = store.Database().Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("l", 32)), nil)
	c := contexts.New(store, a, contexts.DefaultLimits())
	rs := runs.New(store, a, c)
	s := New(store, a, rs, NewRemote(cfg))
	p := access.Principal{UserID: user}
	stamp := time.Now().UTC().Format("20060102T150405")
	d := Draft{Name: "publisher live acceptance", Connection: connection.ID, Title: "[koujou-san #22 接続試験] " + stamp, Body: "koujou-san #22の所有する接続試験です。固定下書きの受付・GitHubでの作成確認・外部の編集観測だけを検証します。", Sources: []Ref{}}
	ref, err := accepted(s.Put(ctx, p, w, "publication-draft:live-"+stamp, Write{Mutation: apicontract.Mutation{OperationID: "live-draft-" + stamp, ExpectedVersion: "0"}, Value: raw(d)}))
	if err != nil {
		t.Fatal(err)
	}
	requestRef, err := accepted(s.Submit(ctx, p, w, "publication:live-"+stamp, Submit{Mutation: apicontract.Mutation{OperationID: "live-submit-" + stamp, ExpectedVersion: "0"}, Draft: ref}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx, w, requestRef.ID); err != nil {
		t.Fatal(err)
	}
	var request Request
	var rec Record
	for attempt := 0; attempt < 7; attempt++ {
		rec, err = s.Read(ctx, p, w, Ref{ID: requestRef.ID, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(rec.Value, &request)
		if request.State == "created" {
			break
		}
		if request.State != "unknown" {
			t.Fatal("external creation not confirmed", request.State, request.Reason)
		}
		if _, err = s.Refresh(ctx, p, w, rec.Ref); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("live confirmation timed out")
		case <-time.After(5 * time.Second):
		}
	}
	rec, err = s.Read(ctx, p, w, Ref{ID: requestRef.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(rec.Value, &request)
	if request.State != "created" || request.External == nil {
		t.Fatal("external result remains unconfirmed")
	}
	issue := request.External
	t.Cleanup(func() {
		cmd := exec.Command("gh", "issue", "close", issue.URL, "--repo", connection.Repository)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("owned issue cleanup failed: %s %v", output, err)
		}
	})
	if issue.Title != d.Title || issue.Body != d.Body+"\n\n"+request.Marker {
		t.Fatal("external fixed input differs")
	}
	bodyFile := filepath.Join(t.TempDir(), "external-edit.md")
	externalBody := issue.Body + "\n\n実接続試験で外部から追記した確認文。"
	if err = os.WriteFile(bodyFile, []byte(externalBody), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "gh", "issue", "edit", issue.URL, "--repo", connection.Repository, "--body-file", bodyFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("owned external edit: %s %v", output, err)
	}
	if _, err = s.Refresh(ctx, p, w, rec.Ref); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Read(ctx, p, w, Ref{ID: rec.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(latest.Value, &request)
	if request.External.Body != externalBody || request.Input.Body != d.Body {
		t.Fatal("external canonical edit not separate from draft")
	}
	t.Logf("live App create/GET confirmation, fixed marker, canonical external edit and unchanged shared draft passed: %s; request=%s; Run=%s", issue.URL, rec.ID, request.Run)
}
