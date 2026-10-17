// Package githubobserve observes GitHub without granting business write access.
package githubobserve

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubapp"
	"os"
	"regexp"
	"strconv"
	"time"
)

type Target struct {
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
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func Environment() (*Config, error) {
	path := os.Getenv("KOUJOU_GITHUB_CONFIG")
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("GitHub configuration unreadable")
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil {
		return nil, errors.New("GitHub configuration invalid")
	}
	if err = c.validate(); err != nil {
		return nil, err
	}
	c.key, err = githubapp.ReadKey(c.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	if c.WebhookSecretFile != "" {
		c.secret, err = os.ReadFile(c.WebhookSecretFile)
		if err != nil || len(c.secret) == 0 {
			return nil, errors.New("GitHub webhook secret unreadable")
		}
	}
	return &c, nil
}
func (c *Config) validate() error {
	bad := errors.New("GitHub configuration invalid")
	if c.AppID == "" {
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
	for _, t := range c.Targets {
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
func (c *Config) interval() time.Duration { return time.Duration(c.IntervalSeconds) * time.Second }
