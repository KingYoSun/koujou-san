package githubobserve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGiteaOnlyEnvironment(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "read-token")
	if err := os.WriteFile(token, []byte("owned-gitea-read-key"), 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"gitea_instances": []any{map[string]any{"url": "https://gitea.example.test", "token_file": token}},
		"targets":         []any{map[string]any{"id": "gitea:private-issue", "provider": "gitea", "instance": "https://gitea.example.test", "repository": "proof/private", "repository_id": 1, "kind": "issue", "external_id": "1"}},
		"bindings":        []any{map[string]any{"workspace_id": "one", "target_id": "gitea:private-issue"}},
	}
	data, _ := json.Marshal(config)
	file := filepath.Join(dir, "gitea.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOUJOU_GITHUB_CONFIG", "")
	t.Setenv("KOUJOU_GITEA_CONFIG", file)
	c, err := Environment()
	if err != nil || c == nil || len(c.Targets) != 1 || c.Targets[0].ID != "gitea:private-issue" {
		t.Fatalf("Gitea-only configuration was ignored: %+v %v", c, err)
	}
}

func TestGiteaLive(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_GITEA_LIVE") != "1" {
		t.Skip("explicit self-hosted Gitea test only")
	}
	c, err := Environment()
	if err != nil || c == nil || len(c.Targets) == 0 {
		t.Fatal("configure KOUJOU_GITEA_CONFIG without exposing credentials")
	}
	r := newRemote(c)
	results := []Snapshot{}
	for _, target := range c.Targets {
		if target.Provider != "gitea" {
			continue
		}
		result, err := r.observe(context.Background(), target)
		if err != nil {
			t.Fatalf("%s: %v", target.ID, err)
		}
		if result.Provider != "gitea" || result.Instance != target.Instance || result.RepositoryID != target.RepositoryID || result.SourceVersion == "" {
			t.Fatal("external identity not fixed")
		}
		results = append(results, result)
		changed := target
		changed.RepositoryID++
		if _, err := r.observe(context.Background(), changed); err == nil || err.Error() != "repository_not_allowed" {
			t.Fatal("changed repository identity accepted")
		}
	}
	value, _ := json.MarshalIndent(results, "", "  ")
	if out := os.Getenv("KOUJOU_GITEA_EVIDENCE"); out != "" {
		if err := os.WriteFile(out, value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("observed %d fixed Gitea targets with explicit CA and repository identity", len(results))
}
