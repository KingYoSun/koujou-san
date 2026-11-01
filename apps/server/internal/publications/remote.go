package publications

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Connection struct {
	Provider       string `json:"provider,omitempty"`
	Instance       string `json:"instance,omitempty"`
	ActorID        int64  `json:"actor_id,omitempty"`
	ID             string `json:"id"`
	Workspace      string `json:"workspace_id"`
	Repository     string `json:"repository"`
	RepositoryID   int64  `json:"repository_id"`
	InstallationID int64  `json:"installation_id"`
}
type Config struct {
	PriorityIntervalSeconds int          `json:"priority_interval_seconds,omitempty"`
	AppID                   string       `json:"app_id"`
	PrivateKeyFile          string       `json:"private_key_file"`
	Connections             []Connection `json:"connections"`
	key                     *rsa.PrivateKey
	GiteaInstances          []gitea.Instance `json:"gitea_instances,omitempty"`
	clients                 map[string]*gitea.Client
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func Environment() (*Config, error) {
	path := os.Getenv("KOUJOU_PUBLICATION_CONFIG")
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrInvalid
	}
	var c Config
	if decode(b, &c) != nil || (c.AppID == "" && len(c.GiteaInstances) == 0) || c.PriorityIntervalSeconds < 0 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	c.clients = map[string]*gitea.Client{}
	for _, instance := range c.GiteaInstances {
		client, err := instance.Open()
		if err != nil || c.clients[client.URL] != nil {
			return nil, ErrInvalid
		}
		c.clients[client.URL] = client
	}
	for index, conn := range c.Connections {
		key := conn.Workspace + "\x00" + conn.ID
		if conn.ID == "" || conn.Workspace == "" || !repositoryName.MatchString(conn.Repository) || conn.RepositoryID <= 0 || seen[key] {
			return nil, ErrInvalid
		}
		if conn.Provider == "gitea" {
			origin, err := gitea.Origin(conn.Instance)
			if err != nil || c.clients[origin] == nil || conn.InstallationID != 0 {
				return nil, ErrInvalid
			}
			conn.Instance = origin
			c.Connections[index] = conn
		} else if conn.Provider != "" || conn.Instance != "" || conn.ActorID != 0 || conn.InstallationID <= 0 || c.AppID == "" {
			return nil, ErrInvalid
		}
		seen[key] = true
	}
	if c.AppID != "" {
		c.key, err = githubapp.ReadKey(c.PrivateKeyFile)
	}
	return &c, err
}

type Remote struct {
	config   *Config
	client   *http.Client
	base     string
	mu       sync.Mutex
	tokens   map[string]githubapp.Token
	resumeAt time.Time
	botID    int64
}

func NewRemote(c *Config) *Remote {
	return &Remote{config: c, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.github.com", tokens: map[string]githubapp.Token{}}
}
func (r *Remote) Connection(w, id string) (Connection, bool) {
	if r == nil || r.config == nil {
		return Connection{}, false
	}
	for _, c := range r.config.Connections {
		if c.Workspace == w && c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}
func (r *Remote) request(ctx context.Context, method, path, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "koujou-san-issue-publication")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := r.client.Do(req)
	if err == nil {
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds > 0 {
			r.resumeAt = time.Now().Add(time.Duration(seconds) * time.Second)
		}
		if response.Header.Get("X-RateLimit-Remaining") == "0" {
			if stamp, e := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); e == nil && time.Unix(stamp, 0).After(r.resumeAt) {
				r.resumeAt = time.Unix(stamp, 0)
			}
		}
	}
	return response, err
}
func (r *Remote) token(ctx context.Context, c Connection) (string, error) {
	if c.Provider == "gitea" {
		client := r.config.clients[c.Instance]
		if client == nil {
			return "", ErrInvalid
		}
		return "", client.Repository(ctx, c.Repository, c.RepositoryID)
	}
	key := c.Workspace + ":" + c.ID
	if t, ok := r.tokens[key]; ok && time.Until(t.Expires) > time.Minute {
		return t.Value, nil
	}
	t, err := githubapp.Exchange(ctx, r.config.AppID, r.config.key, githubapp.Repository{InstallationID: c.InstallationID, ID: c.RepositoryID, Name: c.Repository}, map[string]string{"metadata": "read", "issues": "write"}, r.request)
	if err == nil {
		r.tokens[key] = t
	}
	return t.Value, err
}
func outcome(state, reason string, issue *Issue) Outcome {
	return Outcome{State: state, Reason: reason, Issue: issue, At: time.Now().UTC()}
}
func validIssue(c Connection, i Issue) bool {
	base := "https://github.com"
	if c.Provider == "gitea" {
		base = c.Instance
	}
	return i.ID > 0 && i.Number > 0 && plainIssue(c, i) && i.URL == fmt.Sprintf("%s/%s/issues/%d", base, c.Repository, i.Number) && i.Title != "" && !i.UpdatedAt.IsZero()
}

func plainIssue(c Connection, i Issue) bool {
	return len(i.PullRequest) == 0 || (c.Provider == "gitea" && string(i.PullRequest) == "null")
}

func (r *Remote) send(ctx context.Context, c Connection, method, path, token string, body io.Reader) (*http.Response, error) {
	if c.Provider == "gitea" {
		return r.config.clients[c.Instance].Request(ctx, method, path, body)
	}
	return r.request(ctx, method, path, token, body)
}
func (r *Remote) get(ctx context.Context, c Connection, token string, n int64) Outcome {
	resp, err := r.send(ctx, c, "GET", fmt.Sprintf("/repos/%s/issues/%d", c.Repository, n), token, nil)
	if err != nil {
		return outcome("unknown", "fetch_interrupted", nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return outcome("unknown", "external_unavailable", nil)
	}
	var issue Issue
	if json.NewDecoder(resp.Body).Decode(&issue) != nil || !validIssue(c, issue) || issue.Number != n {
		return outcome("unknown", "invalid_external_response", nil)
	}
	return outcome("created", "", &issue)
}
func (r *Remote) Create(ctx context.Context, c Connection, d Draft, body string) Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.resumeAt) {
		return outcome("failed", "rate_limited_before_send", nil)
	}
	token, err := r.token(ctx, c)
	if err != nil {
		return outcome("failed", "authentication_failed_before_send", nil)
	}
	resp, err := r.send(ctx, c, "POST", "/repos/"+c.Repository+"/issues", token, strings.NewReader(string(raw(map[string]any{"title": d.Title, "body": body}))))
	if err != nil {
		return outcome("unknown", "create_response_lost", nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 410 || resp.StatusCode == 422 {
		return outcome("failed", fmt.Sprintf("creation_rejected_%d", resp.StatusCode), nil)
	}
	var created Issue
	if resp.StatusCode != 201 || json.NewDecoder(resp.Body).Decode(&created) != nil || !validIssue(c, created) {
		return outcome("unknown", "create_response_unconfirmed", nil)
	}
	confirmed := r.get(ctx, c, token, created.Number)
	if confirmed.Issue == nil {
		confirmed.Issue = &created
	}
	return confirmed
}
func (r *Remote) Observe(ctx context.Context, c Connection, tag string, number int64) Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.resumeAt) {
		return outcome("unknown", "rate_limited", nil)
	}
	token, err := r.token(ctx, c)
	if err != nil {
		return outcome("unknown", "authentication_failed", nil)
	}
	if number > 0 {
		return r.get(ctx, c, token, number)
	}
	if c.Provider == "gitea" {
		return r.lookupGitea(ctx, c, tag)
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(tag, "<!-- koujou-san-request:"), " -->")
	query := url.QueryEscape("repo:" + c.Repository + " is:issue in:body " + digest)
	resp, err := r.request(ctx, "GET", "/search/issues?q="+query+"&per_page=2", token, nil)
	if err != nil {
		return outcome("unknown", "lookup_interrupted", nil)
	}
	defer resp.Body.Close()
	var found struct {
		Total      int     `json:"total_count"`
		Incomplete bool    `json:"incomplete_results"`
		Items      []Issue `json:"items"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&found) != nil {
		return outcome("unknown", "lookup_unavailable", nil)
	}
	if found.Incomplete || found.Total != 1 || len(found.Items) != 1 || !validIssue(c, found.Items[0]) {
		return outcome("unknown", "lookup_unconfirmed", nil)
	}
	result := r.get(ctx, c, token, found.Items[0].Number)
	if result.Issue != nil && !strings.Contains(result.Issue.Body, tag) {
		return outcome("unknown", "marker_not_found", nil)
	}
	return result
}
