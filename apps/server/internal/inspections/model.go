// Package inspections fixes the basis and evidence of derived Runs. It does not
// launch processes or infer structured judgments from natural-language output.
package inspections

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

type Ref = access.VersionRef

var ErrInvalid = errors.New("invalid_inspection")
var ErrState = errors.New("inspection_state_conflict")

type Definition struct {
	Name         string             `json:"name"`
	Kind         string             `json:"kind"`    // InspectionRun or HumanReviewRun
	Purpose      string             `json:"purpose"` // contract, fresh, human
	Template     Ref                `json:"template"`
	Targets      []contexts.Fixed   `json:"targets"`
	Criteria     []Criterion        `json:"criteria"`
	Selection    contexts.Selection `json:"selection"`
	Required     map[string]string  `json:"required_separation"`
	Observations map[string][]Ref   `json:"separation_observations,omitempty"`
	Reuse        *Reuse             `json:"reuse,omitempty"`
}
type Criterion struct {
	ID          string        `json:"id"`
	Requirement string        `json:"requirement"`
	Source      *contexts.Ref `json:"source,omitempty"`
}
type Reuse struct {
	Run        Ref      `json:"run"`
	Criteria   []string `json:"criteria"`
	Changes    []string `json:"changes"`
	Impact     []string `json:"impact"`
	Evidence   []Ref    `json:"evidence"`
	Unverified []string `json:"unverified"`
}
type Separation struct {
	Level    string `json:"level"` // unobserved, declared, observed, enforced
	Method   string `json:"method"`
	Origin   string `json:"origin"` // declaration, observation, platform
	Evidence []Ref  `json:"evidence,omitempty"`
}
type Launch struct {
	Definition Ref                   `json:"definition"`
	Template   Ref                   `json:"template"`
	Run        string                `json:"run_id"`
	Manifest   contexts.Manifest     `json:"manifest"`
	Spec       runs.Spec             `json:"spec"`
	Separation map[string]Separation `json:"separation"`
	Device     string                `json:"device_id"`
	Repository *runs.Repository      `json:"repository,omitempty"`
	By         string                `json:"requested_by"`
	At         time.Time             `json:"at"`
}
type Report struct {
	Inspection       Ref               `json:"inspection"`
	ManifestDigest   string            `json:"manifest_digest"`
	Verdict          string            `json:"verdict"`
	Results          []CriterionResult `json:"criteria_results"`
	Unverified       []string          `json:"unverified"`
	RecommendedRoute string            `json:"recommended_route,omitempty"`
}
type CriterionResult struct {
	ID         string   `json:"criterion_id"`
	Verdict    string   `json:"verdict"`
	Evidence   []Ref    `json:"evidence_refs"`
	Unverified []string `json:"unverified"`
	Executed   bool     `json:"executed"`
}
type Evidence struct {
	Artifact    contexts.Fixed   `json:"artifact"`
	Targets     []contexts.Fixed `json:"targets"`
	Criteria    []string         `json:"criteria"`
	Kind        string           `json:"kind"` // machine-observation, human-confirmation, ai-comment, implementer-claim
	Description string           `json:"description"`
	State       string           `json:"state"` // available, unobserved, expired, unavailable
	CheckedAt   time.Time        `json:"checked_at"`
	ExpiresAt   *time.Time       `json:"expires_at,omitempty"`
}
type Judgment struct {
	Launch     Ref       `json:"launch"`
	Source     Ref       `json:"source"`
	Criteria   []string  `json:"criteria"`
	Decision   string    `json:"decision"` // accept-with-exceptions or reject
	Reason     string    `json:"reason"`
	Unverified []string  `json:"unverified"`
	By         string    `json:"by"`
	At         time.Time `json:"at"`
}
type Record struct {
	Ref
	Owner    string          `json:"owner_id"`
	Kind     string          `json:"kind"`
	Position string          `json:"position"`
	Value    json.RawMessage `json:"value"`
	Refs     []Ref           `json:"refs"`
}
type Write struct {
	apicontract.Mutation
	Value     json.RawMessage `json:"value,omitempty"`
	ValueUTF8 *string         `json:"value_utf8,omitempty"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "0" && r.Version != "head"
}
func verdict(v string) bool { return v == "PASS" || v == "FAIL" || v == "INCONCLUSIVE" }

var dimensions = []string{"session", "workspace", "permissions", "external_access", "criteria", "context"}

func rank(level string) int {
	switch level {
	case "declared":
		return 1
	case "observed":
		return 2
	case "enforced":
		return 3
	}
	return 0
}
