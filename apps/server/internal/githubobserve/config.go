// Package githubobserve observes GitHub without granting business write access.
package githubobserve

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Target struct {
	Provider       string `json:"provider,omitempty"`
	Instance       string `json:"instance,omitempty"`
	Workflow       string `json:"workflow,omitempty"`
	ID             string `json:"id"`
	InstallationID int64  `json:"installation_id"`
	RepositoryID   int64  `json:"repository_id"`
	Repository     string `json:"repository"`
	Kind           string `json:"kind"`
	ExternalID     string `json:"external_id"`
	Attempt        int    `json:"attempt,omitempty"`
	WorkflowID     int64  `json:"workflow_id,omitempty"`
	Event          string `json:"event,omitempty"`
}
type Binding struct {
	WorkspaceID    string   `json:"workspace_id"`
	TargetID       string   `json:"target_id"`
	ExpectedCommit string   `json:"expected_commit,omitempty"`
	RequiredJobs   []string `json:"required_jobs,omitempty"`
}
type Config struct {
	AppID             string    `json:"app_id"`
	PrivateKeyFile    string    `json:"private_key_file"`
	WebhookSecretFile string    `json:"webhook_secret_file,omitempty"`
	IntervalSeconds   int       `json:"interval_seconds"`
	Targets           []Target  `json:"targets"`
	Bindings          []Binding `json:"bindings"`
	key               *rsa.PrivateKey
	secret            []byte
	GiteaInstances    []gitea.Instance `json:"gitea_instances,omitempty"`
	clients           map[string]*gitea.Client
	giteaInterval     time.Duration
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Environment() (*Config, error) {
	path := os.Getenv("KOUJOU_GITHUB_CONFIG")
	giteaPath := os.Getenv("KOUJOU_GITEA_CONFIG")
	if path == "" && giteaPath == "" {
		return nil, nil
	}
	var c Config
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, errors.New("GitHub configuration invalid")
		}
	}
	if giteaPath != "" {
		raw, err := os.ReadFile(giteaPath)
		var extra Config
		if err != nil || json.Unmarshal(raw, &extra) != nil || len(extra.GiteaInstances) == 0 || extra.IntervalSeconds < 0 {
			return nil, gitea.ErrConfig
		}
		if extra.IntervalSeconds == 0 {
			extra.IntervalSeconds = 60
		}
		c.giteaInterval = time.Duration(extra.IntervalSeconds) * time.Second
		c.GiteaInstances = append(c.GiteaInstances, extra.GiteaInstances...)
		for _, target := range extra.Targets {
			if target.Provider != "gitea" {
				return nil, gitea.ErrConfig
			}
			c.Targets = append(c.Targets, target)
		}
		c.Bindings = append(c.Bindings, extra.Bindings...)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if c.AppID != "" {
		key, err := githubapp.ReadKey(c.PrivateKeyFile)
		if err != nil {
			return nil, err
		}
		c.key = key
	}
	if c.WebhookSecretFile != "" {
		secret, err := os.ReadFile(c.WebhookSecretFile)
		if err != nil || len(secret) == 0 {
			return nil, errors.New("GitHub webhook secret unreadable")
		}
		c.secret = secret
	}
	return &c, nil
}
func (c *Config) validate() error {
	bad := errors.New("GitHub configuration invalid")
	if c.AppID == "" && len(c.GiteaInstances) == 0 {
		return bad
	}
	if c.IntervalSeconds == 0 {
		c.IntervalSeconds = 60
	}
	if c.IntervalSeconds < 1 {
		return bad
	}
	ids := map[string]bool{}
	tuples := map[string]bool{}
	c.clients = map[string]*gitea.Client{}
	for _, instance := range c.GiteaInstances {
		client, err := instance.Open()
		if err != nil || c.clients[client.URL] != nil {
			return gitea.ErrConfig
		}
		c.clients[client.URL] = client
	}
	for index, t := range c.Targets {
		if t.Provider == "gitea" {
			origin, err := gitea.Origin(t.Instance)
			if err != nil {
				return err
			}
			t.Instance = origin
			c.Targets[index] = t
			if err := c.validateGitea(t, ids, tuples); err != nil {
				return err
			}
			continue
		}
		if t.Provider != "" || t.Instance != "" || t.Workflow != "" || c.AppID == "" {
			return bad
		}
		if !idPattern.MatchString(t.ID) || ids[t.ID] || t.InstallationID <= 0 || t.RepositoryID <= 0 || !repositoryPattern.MatchString(t.Repository) || t.Attempt < 0 {
			return bad
		}
		if t.WorkflowID != 0 || t.Event != "" {
			if t.Kind != "actions" || t.WorkflowID < 1 || t.ExternalID != "" || t.Attempt != 0 || (t.Event != "" && !idPattern.MatchString(t.Event)) {
				return bad
			}
		} else if t.Kind == "commit" {
			if !shaPattern.MatchString(t.ExternalID) {
				return bad
			}
		} else {
			if t.Kind != "issue" && t.Kind != "pr" && t.Kind != "actions" {
				return bad
			}
			n, e := strconv.ParseInt(t.ExternalID, 10, 64)
			if e != nil || n <= 0 {
				return bad
			}
		}
		if t.Kind != "actions" && t.Attempt != 0 {
			return bad
		}
		tuple := strconv.FormatInt(t.InstallationID, 10) + ":" + strconv.FormatInt(t.RepositoryID, 10) + ":" + t.Kind + ":" + t.ExternalID + ":" + strconv.Itoa(t.Attempt) + ":" + strconv.FormatInt(t.WorkflowID, 10) + ":" + t.Event
		if tuples[tuple] {
			return errors.New("reuse one GitHub target for identical external references")
		}
		tuples[tuple] = true
		ids[t.ID] = true
	}
	seen := map[string]bool{}
	for _, b := range c.Bindings {
		key := b.WorkspaceID + ":" + b.TargetID
		if b.WorkspaceID == "" || !ids[b.TargetID] || seen[key] || (b.ExpectedCommit != "" && !shaPattern.MatchString(b.ExpectedCommit)) {
			return bad
		}
		seen[key] = true
	}
	return nil
}
func (c *Config) interval(t Target) time.Duration {
	if t.Provider == "gitea" && c.giteaInterval > 0 {
		return c.giteaInterval
	}
	return time.Duration(c.IntervalSeconds) * time.Second
}

func (c *Config) validateGitea(t Target, ids, tuples map[string]bool) error {
	origin, err := gitea.Origin(t.Instance)
	if err != nil || origin != t.Instance || c.clients[origin] == nil || !strings.HasPrefix(t.ID, "gitea:") || !idPattern.MatchString(strings.TrimPrefix(t.ID, "gitea:")) || ids[t.ID] || !repositoryPattern.MatchString(t.Repository) || t.RepositoryID <= 0 || t.InstallationID != 0 || t.WorkflowID != 0 || t.Attempt < 0 {
		return gitea.ErrConfig
	}
	if t.Workflow != "" {
		if t.Kind != "actions" || t.ExternalID != "" || t.Attempt != 0 || strings.ContainsAny(t.Workflow, "/\\?#") {
			return gitea.ErrConfig
		}
	} else if t.Kind == "commit" || t.Kind == "status" {
		if !shaPattern.MatchString(t.ExternalID) {
			return gitea.ErrConfig
		}
	} else {
		n, e := strconv.ParseInt(t.ExternalID, 10, 64)
		if (t.Kind != "issue" && t.Kind != "pr" && t.Kind != "actions") || e != nil || n <= 0 {
			return gitea.ErrConfig
		}
	}
	if t.Kind != "actions" && (t.Attempt != 0 || t.Event != "") {
		return gitea.ErrConfig
	}
	identity := t
	identity.ID = ""
	tuple, _ := json.Marshal(identity)
	if tuples[string(tuple)] {
		return gitea.ErrConfig
	}
	tuples[string(tuple)], ids[t.ID] = true, true
	return nil
}
