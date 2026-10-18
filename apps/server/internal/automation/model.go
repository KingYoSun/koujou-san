// Package automation persists accepted work, leases and versioned triage state.
// Executors are supplied by the owning Hopper/Triage domains, never by the wire.
package automation

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
)

var ErrInvalid = errors.New("invalid_automation")
var ErrLease = errors.New("stale_lease")

type Ref = access.VersionRef
type Level struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Meaning string `json:"meaning"`
}

func DefaultLevels() []Level {
	return []Level{{"P0", "緊急", "直ちに対応する"}, {"P1", "高", "通常の作業より先に対応する"}, {"P2", "通常", "通常の順序で対応する"}, {"P3", "低", "他の作業を優先できる"}}
}

type Policy struct {
	External   *ExternalPolicy `json:"external,omitempty"`
	Model      *serverllm.Spec `json:"model,omitempty"`
	Name       string          `json:"name"`
	Scopes     []string        `json:"scopes"`
	Precedence int             `json:"precedence"`
	Levels     []Level         `json:"levels"`
	Criteria   string          `json:"criteria"`
	Context    []Ref           `json:"context"`
}
type Activation struct {
	Policy    Ref  `json:"policy"`
	Enabled   bool `json:"enabled"`
	AutoApply bool `json:"auto_apply"`
}
type Budget struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Valuation string `json:"valuation"`
}
type Control struct {
	Kind           string  `json:"kind"`
	Controller     string  `json:"controller"`
	Enabled        bool    `json:"enabled"`
	CanRead        bool    `json:"can_read"`
	CanCommit      bool    `json:"can_commit"`
	Slots          int     `json:"slots"`
	MaxAttempts    int     `json:"max_attempts"`
	LeaseSeconds   int     `json:"lease_seconds"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	RetrySeconds   int     `json:"retry_seconds"`
	MaxRequests    int     `json:"max_requests"`
	PeriodSeconds  int     `json:"period_seconds"`
	Budget         *Budget `json:"budget"`
}
type Priority struct {
	AdoptedPolicy        *Ref    `json:"adopted_policy,omitempty"`
	Basis                string  `json:"basis,omitempty"`
	Target               Ref     `json:"target"`
	Policy               Ref     `json:"policy"`
	Adopted              *string `json:"adopted"`
	Origin               string  `json:"origin"`
	Fixed                bool    `json:"fixed"`
	Recommendation       *string `json:"recommendation"`
	RecommendationTarget *Ref    `json:"recommendation_target"`
	RecommendationPolicy *Ref    `json:"recommendation_policy"`
	Reason               string  `json:"reason"`
	AdoptedReason        string  `json:"adopted_reason"`
	RecommendationReason string  `json:"recommendation_reason"`
	State                string  `json:"state"`
	Stale                bool    `json:"stale"`
	Job                  string  `json:"job,omitempty"`
	Refs                 []Ref   `json:"refs"`
}
type PriorityEdit struct {
	Target Ref     `json:"target"`
	Policy Ref     `json:"policy"`
	Value  *string `json:"value"`
	Reason string  `json:"reason"`
	Unlock bool    `json:"unlock"`
}
type Record struct {
	Ref
	Kind     string          `json:"kind"`
	Position int64           `json:"position"`
	Value    json.RawMessage `json:"value"`
	Refs     []Ref           `json:"refs"`
}
type Write struct {
	apicontract.Mutation
	Value json.RawMessage `json:"value"`
}
type Source struct {
	Event  string   `json:"event"`
	Target Ref      `json:"target"`
	Scopes []string `json:"scopes"`
}
type Result struct {
	State      string  `json:"state,omitempty"`
	Basis      string  `json:"basis,omitempty"`
	Priority   *string `json:"priority"`
	Reason     string  `json:"reason"`
	Refs       []Ref   `json:"refs"`
	Usage      []Ref   `json:"usage"`
	UsageState string  `json:"usage_state"`
}
type Job struct {
	ID         string          `json:"id"`
	Workspace  string          `json:"workspace_id"`
	Kind       string          `json:"kind"`
	Event      string          `json:"event"`
	Target     Ref             `json:"target"`
	Definition Ref             `json:"definition"`
	State      string          `json:"state"`
	Reason     string          `json:"reason"`
	Worker     string          `json:"worker"`
	Lease      string          `json:"lease"`
	LeaseUntil *time.Time      `json:"lease_until"`
	Attempts   int             `json:"attempts"`
	DueAt      time.Time       `json:"due_at"`
	Deadline   *time.Time      `json:"deadline"`
	Checkpoint json.RawMessage `json:"-"`
	Result     *Result         `json:"result"`
	Refs       []Ref           `json:"refs"`
	CreatedAt  time.Time       `json:"created_at"`
}

func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "head" && r.Version != "0"
}
func hasLevel(p Policy, id string) bool {
	for _, v := range p.Levels {
		if v.ID == id {
			return true
		}
	}
	return false
}
func validPolicy(p Policy) bool {
	if p.Model != nil && !serverllm.Valid(p.Model) {
		return false
	}
	if p.Name == "" || p.Criteria == "" || len(p.Scopes) == 0 || len(p.Levels) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, l := range p.Levels {
		if l.ID == "" || l.Name == "" || seen[l.ID] {
			return false
		}
		seen[l.ID] = true
	}
	for _, r := range p.Context {
		if !fixed(r) {
			return false
		}
	}
	return true
}
func validControl(c Control) bool {
	return (c.Kind == "hopper" || c.Kind == "triage") && c.Slots > 0 && c.MaxAttempts > 0 && c.LeaseSeconds > 0 && c.TimeoutSeconds > 0 && c.TimeoutSeconds <= c.LeaseSeconds && c.RetrySeconds >= 0 && c.MaxRequests > 0 && c.PeriodSeconds > 0
}
