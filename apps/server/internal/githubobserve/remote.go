package githubobserve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Attempt    int    `json:"run_attempt"`
}
type Snapshot struct {
	Provider        string `json:"provider,omitempty"`
	Kind            string `json:"kind,omitempty"`
	Instance        string `json:"instance,omitempty"`
	Repository      string `json:"repository,omitempty"`
	RepositoryID    int64  `json:"repository_id,omitempty"`
	Workflow        string `json:"workflow,omitempty"`
	ServerVersion   string `json:"server_version,omitempty"`
	URL             string `json:"url"`
	Title           string `json:"title,omitempty"`
	SourceUpdatedAt string `json:"source_updated_at,omitempty"`
	SourceVersion   string `json:"source_version"`
	State           string `json:"state"`
	Commit          string `json:"commit,omitempty"`
	MergeCommit     string `json:"merge_commit,omitempty"`
	WorkflowID      int64  `json:"workflow_id,omitempty"`
	RunID           int64  `json:"run_id,omitempty"`
	Attempt         int    `json:"run_attempt,omitempty"`
	Conclusion      string `json:"conclusion,omitempty"`
	Jobs            []Job  `json:"jobs,omitempty"`
}
type source struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	URL         string `json:"html_url"`
	Updated     string `json:"updated_at"`
	SHA         string `json:"sha"`
	HeadSHA     string `json:"head_sha"`
	WorkflowID  int64  `json:"workflow_id"`
	Path        string `json:"path"`
	Attempt     int    `json:"run_attempt"`
	MergeCommit string `json:"merge_commit_sha"`
	Merged      bool   `json:"merged"`
	Head        struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Commit struct {
		Message   string `json:"message"`
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}
type cachedResponse struct {
	ETag string
	Body []byte
}
type appToken = githubapp.Token

// Remote is owned by the single observation worker. HTTP calls are serialized,
// including token exchange, page retrieval and webhook-triggered reconciliations.
type Remote struct {
	config       *Config
	client       *http.Client
	base         string
	tokens       map[string]appToken
	cache        map[string]cachedResponse
	workflowRuns map[string]int64
	resumeAt     time.Time
}

func newRemote(c *Config) *Remote {
	return &Remote{config: c, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.github.com", tokens: map[string]appToken{}, cache: map[string]cachedResponse{}, workflowRuns: map[string]int64{}}
}
func (r *Remote) request(ctx context.Context, method, path, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "koujou-san-observation")
	if method == "GET" {
		if c, ok := r.cache[path]; ok && c.ETag != "" {
			req.Header.Set("If-None-Match", c.ETag)
		}
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	return r.client.Do(req)
}
func (r *Remote) token(ctx context.Context, t Target) (string, error) {
	key := fmt.Sprintf("%d:%d", t.InstallationID, t.RepositoryID)
	if tok, ok := r.tokens[key]; ok && time.Until(tok.Expires) > time.Minute {
		if !strings.EqualFold(tok.Repository, t.Repository) {
			return "", errors.New("repository_not_allowed")
		}
		return tok.Value, nil
	}
	tok, err := githubapp.Exchange(ctx, r.config.AppID, r.config.key, githubapp.Repository{InstallationID: t.InstallationID, ID: t.RepositoryID, Name: t.Repository}, map[string]string{"metadata": "read", "issues": "read", "pull_requests": "read", "contents": "read", "actions": "read"}, func(ctx context.Context, method, path, token string, body io.Reader) (*http.Response, error) {
		resp, err := r.request(ctx, method, path, token, body)
		if err == nil {
			r.limit(resp)
		}
		return resp, err
	})
	if err != nil {
		return "", err
	}
	r.tokens[key] = tok
	return tok.Value, nil
}
func (r *Remote) limit(resp *http.Response) {
	now := time.Now()
	next := now
	if seconds, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && seconds > 0 {
		next = now.Add(time.Duration(seconds) * time.Second)
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if n, e := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); e == nil && time.Unix(n, 0).After(next) {
			next = time.Unix(n, 0)
		}
	}
	if (resp.StatusCode == 403 || resp.StatusCode == 429) && !next.After(now) {
		next = now.Add(time.Minute)
	}
	if next.After(r.resumeAt) {
		r.resumeAt = next
	}
}
func (r *Remote) get(ctx context.Context, t Target, path string, into any) error {
	if t.Provider == "gitea" {
		return r.config.clients[t.Instance].Get(ctx, path, into)
	}
	if time.Now().Before(r.resumeAt) {
		return errors.New("rate_limited")
	}
	token, err := r.token(ctx, t)
	if err != nil {
		return err
	}
	resp, err := r.request(ctx, "GET", path, token, nil)
	if err != nil {
		return errors.New("connection_failed")
	}
	defer resp.Body.Close()
	r.limit(resp)
	var data []byte
	if resp.StatusCode == 304 {
		data = r.cache[path].Body
	} else if resp.StatusCode == 200 {
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return errors.New("fetch_interrupted")
		}
		if json.Valid(data) {
			r.cache[path] = cachedResponse{resp.Header.Get("ETag"), data}
		}
	} else {
		if resp.StatusCode == 401 {
			delete(r.tokens, fmt.Sprintf("%d:%d", t.InstallationID, t.RepositoryID))
			return errors.New("authentication_failed")
		}
		if resp.StatusCode == 403 || resp.StatusCode == 404 {
			return errors.New("not_accessible")
		}
		if resp.StatusCode == 429 {
			return errors.New("rate_limited")
		}
		return errors.New("source_unavailable")
	}
	if json.Unmarshal(data, into) != nil {
		return errors.New("invalid_source_response")
	}
	return nil
}
func (r *Remote) observe(ctx context.Context, t Target) (Snapshot, error) {
	var s Snapshot
	pageSize := "per_page"
	if t.Provider == "gitea" {
		client := r.config.clients[t.Instance]
		if client == nil {
			return s, errors.New("not_configured")
		}
		if err := client.Repository(ctx, t.Repository, t.RepositoryID); err != nil {
			return s, err
		}
		var version struct {
			Version string `json:"version"`
		}
		if err := client.Get(ctx, "/version", &version); err != nil {
			return s, err
		}
		s = Snapshot{Provider: "gitea", Kind: t.Kind, Instance: client.URL, Repository: t.Repository, RepositoryID: t.RepositoryID, ServerVersion: version.Version}
		pageSize = "limit"
		if t.Kind == "status" {
			if err := r.observeStatus(ctx, t, &s); err != nil {
				return Snapshot{}, err
			}
			return finishSnapshot(s)
		}
	}
	prefix := "/repos/" + t.Repository
	if t.WorkflowID > 0 || t.Workflow != "" {
		workflow := strconv.FormatInt(t.WorkflowID, 10)
		if t.Provider == "gitea" {
			workflow = t.Workflow
		}
		path := fmt.Sprintf("%s/actions/workflows/%s/runs?%s=1", prefix, url.PathEscape(workflow), pageSize)
		if t.Event != "" {
			path += "&event=" + url.QueryEscape(t.Event)
		}
		var listing struct {
			Runs []source `json:"workflow_runs"`
		}
		if err := r.get(ctx, t, path, &listing); err != nil {
			return Snapshot{}, err
		}
		if len(listing.Runs) != 1 || listing.Runs[0].ID < 1 || (t.Provider == "" && listing.Runs[0].WorkflowID != t.WorkflowID) {
			return Snapshot{}, errors.New("workflow_result_unavailable")
		}
		run := listing.Runs[0].ID
		if old := r.workflowRuns[t.ID]; t.Provider == "" && old != 0 && old != run {
			previous := fmt.Sprintf("%s/actions/runs/%d", prefix, old)
			for key := range r.cache {
				if key == previous || strings.HasPrefix(key, previous+"/") {
					delete(r.cache, key)
				}
			}
		}
		r.workflowRuns[t.ID] = run
		t.ExternalID = strconv.FormatInt(run, 10)
	}
	path := prefix
	switch t.Kind {
	case "issue":
		path += "/issues/" + t.ExternalID
	case "pr":
		path += "/pulls/" + t.ExternalID
	case "commit":
		path += "/commits/" + t.ExternalID
	case "actions":
		path += "/actions/runs/" + t.ExternalID
		if t.Attempt > 0 {
			path += "/attempts/" + strconv.Itoa(t.Attempt)
		}
	}
	var v source
	if err := r.get(ctx, t, path, &v); err != nil {
		return Snapshot{}, err
	}
	s.URL, s.Title, s.SourceUpdatedAt, s.State = v.URL, v.Title, v.Updated, v.State
	switch t.Kind {
	case "pr":
		s.Commit = v.Head.SHA
		s.MergeCommit = v.MergeCommit
		if v.Merged {
			s.State = "merged"
		}
	case "commit":
		s.Commit = v.SHA
		s.Title = strings.Split(v.Commit.Message, "\n")[0]
		s.SourceUpdatedAt = v.Commit.Committer.Date
		s.State = "available"
	case "actions":
		s.State = v.Status
		s.Commit = v.HeadSHA
		s.WorkflowID = v.WorkflowID
		s.RunID = v.ID
		s.Attempt = v.Attempt
		s.Conclusion = v.Conclusion
		if t.Provider == "gitea" {
			s.Workflow = v.Path
		}
		if s.Attempt < 1 || s.RunID == 0 || (s.WorkflowID == 0 && s.Workflow == "") || s.Commit == "" {
			return Snapshot{}, errors.New("invalid_source_response")
		}
		for page := 1; ; page++ {
			var jobs struct {
				Total int   `json:"total_count"`
				Jobs  []Job `json:"jobs"`
			}
			if err := r.get(ctx, t, fmt.Sprintf("%s/actions/runs/%s/attempts/%d/jobs?%s=100&page=%d", prefix, t.ExternalID, s.Attempt, pageSize, page), &jobs); err != nil {
				return Snapshot{}, err
			}
			for _, j := range jobs.Jobs {
				if j.Attempt != s.Attempt {
					return Snapshot{}, errors.New("changed_during_read")
				}
			}
			s.Jobs = append(s.Jobs, jobs.Jobs...)
			if len(s.Jobs) >= jobs.Total {
				break
			}
			if len(jobs.Jobs) == 0 {
				return Snapshot{}, errors.New("fetch_interrupted")
			}
		}
		var current source
		if err := r.get(ctx, t, path, &current); err != nil {
			return Snapshot{}, err
		}
		if current.Attempt != v.Attempt || current.Status != v.Status || current.Conclusion != v.Conclusion || current.Updated != v.Updated {
			return Snapshot{}, errors.New("changed_during_read")
		}
	}
	return finishSnapshot(s)
}

func finishSnapshot(s Snapshot) (Snapshot, error) {
	if s.URL == "" || s.State == "" || (s.Provider == "gitea" && !gitea.SourceURL(s.Instance, s.Repository, s.URL)) {
		return Snapshot{}, errors.New("invalid_source_response")
	}
	raw, _ := json.Marshal(s)
	hash := sha256.Sum256(raw)
	s.SourceVersion = hex.EncodeToString(hash[:])
	return s, nil
}
