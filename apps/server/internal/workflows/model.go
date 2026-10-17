// Package workflows owns versioned definitions and the progression between Runs.
package workflows

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

var ErrInvalid = errors.New("invalid_workflow")
var ErrState = errors.New("workflow_state_conflict")

// The same JSON value is edited in the GUI, imported, exported and sent by API.
// Run definitions use Spec; workflow definitions use Nodes and Budget.
type Definition struct {
	Schema    string              `json:"schema"`
	Name      string              `json:"name"`
	Spec      *runs.Spec          `json:"spec,omitempty"`
	Selection *contexts.Selection `json:"selection,omitempty"`
	Inputs    []string            `json:"inputs,omitempty"`
	Required  []string            `json:"required,omitempty"`
	DeviceID  string              `json:"device_id,omitempty"`
	CI        *CICommand          `json:"ci,omitempty"`
	Write     *WriteGuard         `json:"write,omitempty"`
	Nodes     []Node              `json:"nodes,omitempty"`
	Budget    *Budget             `json:"budget,omitempty"`
	Baseline  *contexts.Ref       `json:"baseline,omitempty"`
}
type Node struct {
	ID         string            `json:"id"`
	Definition *Ref              `json:"definition,omitempty"`
	Choices    []string          `json:"choices,omitempty"`
	Depends    []Dependency      `json:"depends,omitempty"`
	Join       string            `json:"join,omitempty"`
	Bindings   []Binding         `json:"bindings,omitempty"`
	Inputs     []string          `json:"inputs,omitempty"`
	Retry      map[string]string `json:"retry,omitempty"`
}
type Dependency struct {
	Node     string   `json:"node"`
	On       []string `json:"on"`
	Required bool     `json:"required"`
}
type Binding struct {
	From      string   `json:"from"`
	Kind      string   `json:"kind"`
	Channel   string   `json:"channel,omitempty"`
	Index     int      `json:"index"`
	Path      []string `json:"path"`
	To        string   `json:"to"`
	Parameter []string `json:"parameter,omitempty"`
}
type Budget struct {
	MaxRuns  int         `json:"max_runs"`
	Parallel int         `json:"parallel"`
	Seconds  int         `json:"seconds"`
	Cost     *CostBudget `json:"cost,omitempty"`
}
type CostBudget struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Valuation string `json:"valuation"`
}
type CICommand struct {
	Target       string   `json:"target"`
	Commit       string   `json:"commit"`
	RunID        string   `json:"run_id"`
	RequiredJobs []string `json:"required_jobs"`
}
type WriteGuard struct {
	Target   string       `json:"target"`
	Commit   string       `json:"commit"`
	Required []Dependency `json:"required"`
	// Conditional is a declared connection capability, not a claim that arbitrary
	// CLIs implement conditional external writes.
	Conditional bool     `json:"conditional"`
	Parameter   []string `json:"parameter"`
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
type Problem struct {
	Path string `json:"path"`
	Code string `json:"code"`
}
type Validation struct {
	Definition Ref       `json:"definition"`
	Valid      bool      `json:"valid"`
	Problems   []Problem `json:"problems"`
	Checked    []Ref     `json:"checked"`
}
type Adoption struct {
	Definition Ref `json:"definition"`
	Validation Ref `json:"validation"`
}
type Execution struct {
	Definition Ref                                   `json:"definition"`
	Baseline   *contexts.Ref                         `json:"baseline,omitempty"`
	Controller string                                `json:"controller"`
	State      string                                `json:"state"`
	Reason     string                                `json:"reason,omitempty"`
	StartedAt  time.Time                             `json:"started_at"`
	Deadline   *time.Time                            `json:"deadline,omitempty"`
	Count      int                                   `json:"count"`
	Inputs     map[string]map[string]json.RawMessage `json:"inputs,omitempty"`
	Previous   *Ref                                  `json:"previous,omitempty"`
	Cost       *CostState                            `json:"cost,omitempty"`
}
type Task struct {
	Node              string           `json:"node"`
	Attempt           int              `json:"attempt"`
	Definition        *Ref             `json:"definition,omitempty"`
	Run               *Ref             `json:"run,omitempty"`
	State             string           `json:"state"`
	Outcome           string           `json:"outcome,omitempty"`
	Inputs            []Ref            `json:"inputs,omitempty"`
	DecisionBy        string           `json:"decision_by,omitempty"`
	DecisionAt        *time.Time       `json:"decision_at,omitempty"`
	Prepared          *Prepared        `json:"prepared,omitempty"`
	Repository        *runs.Repository `json:"repository,omitempty"`
	Retried           bool             `json:"retried,omitempty"`
	Previous          *Ref             `json:"previous,omitempty"`
	Unstarted         bool             `json:"-"`
	AssignmentProfile *Ref             `json:"assignment_profile,omitempty"`
	Stale             bool             `json:"stale,omitempty"`
}

type Prepared struct {
	Judgment bool               `json:"judgment"`
	Spec     runs.Spec          `json:"spec"`
	Manifest *contexts.Manifest `json:"manifest,omitempty"`
	Sources  []Ref              `json:"sources"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "0" && r.Version != "head"
}
