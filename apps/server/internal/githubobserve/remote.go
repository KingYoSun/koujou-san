package githubobserve

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
type appToken struct {
	Value      string
	Expires    time.Time
	Repository string
}

// Remote is owned by the single observation worker. HTTP calls are serialized,
// including token exchange, page retrieval and webhook-triggered reconciliations.
type Remote struct {
	config   *Config
	client   *http.Client
	base     string
	tokens   map[string]appToken
	cache    map[string]cachedResponse
	resumeAt time.Time
}

func newRemote(c *Config) *Remote {
	return &Remote{config: c, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.github.com", tokens: map[string]appToken{}, cache: map[string]cachedResponse{}}
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
	enc := base64.RawURLEncoding.EncodeToString
	claims, _ := json.Marshal(map[string]any{"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(8 * time.Minute).Unix(), "iss": r.config.AppID})
	unsigned := enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc(claims)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, r.config.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("authentication_failed")
	}
	payload, _ := json.Marshal(map[string]any{"repository_ids": []int64{t.RepositoryID}, "permissions": map[string]string{"metadata": "read", "issues": "read", "pull_requests": "read", "contents": "read", "actions": "read"}})
	resp, err := r.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", t.InstallationID), unsigned+"."+enc(sig), strings.NewReader(string(payload)))
	if err != nil {
		return "", errors.New("connection_failed")
	}
	defer resp.Body.Close()
	r.limit(resp)
	if resp.StatusCode != 201 {
		return "", errors.New("authentication_failed")
	}
	var tok struct {
		Token        string            `json:"token"`
		Expires      time.Time         `json:"expires_at"`
		Permissions  map[string]string `json:"permissions"`
		Repositories []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if json.NewDecoder(resp.Body).Decode(&tok) != nil || tok.Token == "" || !tok.Expires.After(time.Now()) {
		return "", errors.New("authentication_failed")
	}
	// The exchange explicitly requests just this repository and read permissions.
	for _, v := range tok.Permissions {
		if v != "read" {
			return "", errors.New("unexpected_permissions")
		}
	}
	if len(tok.Repositories) != 1 || tok.Repositories[0].ID != t.RepositoryID || !strings.EqualFold(tok.Repositories[0].FullName, t.Repository) {
		return "", errors.New("repository_not_allowed")
	}
	r.tokens[key] = appToken{tok.Token, tok.Expires, tok.Repositories[0].FullName}
	return tok.Token, nil
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
	prefix := "/repos/" + t.Repository
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
	s := Snapshot{URL: v.URL, Title: v.Title, SourceUpdatedAt: v.Updated, State: v.State}
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
		if s.Attempt < 1 || s.RunID == 0 || s.WorkflowID == 0 || s.Commit == "" {
			return Snapshot{}, errors.New("invalid_source_response")
		}
		for page := 1; ; page++ {
			var jobs struct {
				Total int   `json:"total_count"`
				Jobs  []Job `json:"jobs"`
			}
			if err := r.get(ctx, t, fmt.Sprintf("%s/actions/runs/%s/attempts/%d/jobs?per_page=100&page=%d", prefix, t.ExternalID, s.Attempt, page), &jobs); err != nil {
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
	if s.URL == "" || s.State == "" {
		return Snapshot{}, errors.New("invalid_source_response")
	}
	raw, _ := json.Marshal(s)
	hash := sha256.Sum256(raw)
	s.SourceVersion = hex.EncodeToString(hash[:])
	return s, nil
}
