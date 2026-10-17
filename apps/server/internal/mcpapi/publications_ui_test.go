package mcpapi

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"sync"
	"time"
)

// The GUI fixture has no production GitHub credentials and cannot write to GitHub.
// The App token/HTTP protocol is exercised separately by test:publications.
type publicationUIBackend struct {
	mu     sync.Mutex
	issues map[string]publications.Issue
	count  int64
}

func (b *publicationUIBackend) Connection(w, id string) (publications.Connection, bool) {
	return publications.Connection{ID: "ui-issues", Workspace: "mcp-one", Repository: "fixture/repo", RepositoryID: 8, InstallationID: 7}, w == "mcp-one" && id == "ui-issues"
}
func (b *publicationUIBackend) Create(ctx context.Context, c publications.Connection, d publications.Draft, body string) publications.Outcome {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if b.issues == nil {
		b.issues = map[string]publications.Issue{}
	}
	issue := publications.Issue{ID: b.count + 100, Number: b.count, URL: "https://github.com/fixture/repo/issues/1", Title: d.Title, Body: body, State: "open", UpdatedAt: time.Now().UTC()}
	b.issues[body] = issue
	return publications.Outcome{State: "unknown", Reason: "owned_fixture_response_lost", At: time.Now().UTC()}
}
func (b *publicationUIBackend) Observe(ctx context.Context, c publications.Connection, marker string, n int64) publications.Outcome {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, issue := range b.issues {
		if n == issue.Number || n == 0 && len(issue.Body) >= len(marker) && issue.Body[len(issue.Body)-len(marker):] == marker {
			return publications.Outcome{State: "created", Issue: &issue, At: time.Now().UTC()}
		}
	}
	return publications.Outcome{State: "unknown", Reason: "owned_fixture_unconfirmed", At: time.Now().UTC()}
}
