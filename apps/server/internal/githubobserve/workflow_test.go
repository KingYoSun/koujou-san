package githubobserve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkflowLatestObservation(t *testing.T) {
	target := Target{ID: "nightly", InstallationID: 7, RepositoryID: 8, Repository: "fixture/repo", Kind: "actions", WorkflowID: 42, Event: "schedule"}
	config := &Config{AppID: "fixture", Targets: []Target{target}}
	if err := config.validate(); err != nil {
		t.Fatal(err)
	}
	var latest atomic.Int64
	latest.Store(123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("unexpected write or authority %s", r.Method)
			w.WriteHeader(403)
			return
		}
		if r.URL.Path == "/repos/fixture/repo/actions/workflows/42/runs" {
			if r.URL.Query().Get("per_page") != "1" || r.URL.Query().Get("event") != "schedule" {
				t.Error("unbounded or wrong workflow query")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []source{{ID: latest.Load(), WorkflowID: 42}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "jobs": []Job{{ID: 1, Name: "nightly", Status: "completed", Conclusion: "failure", Attempt: 1}}})
			return
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/fixture/repo/actions/runs/"), 10, 64)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(source{ID: id, WorkflowID: 42, Attempt: 1, HeadSHA: fixtureSHA, Status: "completed", Conclusion: "failure", URL: fmt.Sprintf("https://github.com/fixture/repo/actions/runs/%d", id)})
	}))
	defer server.Close()
	remote := newRemote(config)
	remote.base = server.URL
	remote.tokens["7:8"] = appToken{Value: "fixture-token", Expires: time.Now().Add(time.Hour), Repository: "fixture/repo"}
	first, err := remote.observe(context.Background(), target)
	if err != nil || first.RunID != 123 {
		t.Fatal(first, err)
	}
	latest.Store(124)
	second, err := remote.observe(context.Background(), target)
	if err != nil || second.RunID != 124 || first.SourceVersion == second.SourceVersion {
		t.Fatal(second, err)
	}
	for path := range remote.cache {
		if strings.Contains(path, "/actions/runs/123") {
			t.Fatal("previous-cycle HTTP body retained", path)
		}
	}
	invalid := target
	invalid.ExternalID = "123"
	config.Targets = []Target{invalid}
	if config.validate() == nil {
		t.Fatal("ambiguous fixed run and workflow selector accepted")
	}
}
