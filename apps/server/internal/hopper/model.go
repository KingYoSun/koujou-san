// Package hopper evaluates accepted results and publishes through normal domains.
package hopper

import (
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"time"
)

type Ref = access.VersionRef

var ErrInvalid = errors.New("invalid_hopper")
var ErrState = errors.New("hopper_state_conflict")

type Condition struct {
	All   []Condition     `json:"all,omitempty"`
	Any   []Condition     `json:"any,omitempty"`
	Not   *Condition      `json:"not,omitempty"`
	Field string          `json:"field,omitempty"`
	Op    string          `json:"op,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}
type Facts struct {
	State      string          `json:"state,omitempty"`
	ExitCode   *int            `json:"exit_code,omitempty"`
	Verdict    string          `json:"verdict,omitempty"`
	Conclusion string          `json:"conclusion,omitempty"`
	Report     json.RawMessage `json:"report,omitempty"`
}
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Bounds struct {
	Depth         int               `json:"depth"`
	Publications  int               `json:"publications"`
	Evaluations   int               `json:"evaluations"`
	Seconds       int               `json:"seconds"`
	PerEvaluation int               `json:"per_evaluation"`
	Cost          *usage.CostBudget `json:"cost,omitempty"`
}
type Action struct {
	Kind     string `json:"kind"`
	Template Ref    `json:"template"`
	Start    bool   `json:"start"`
	// Mapping copies only explicitly selected result values into declared channels.
	Mappings   []Mapping          `json:"mappings"`
	Parent     *contexts.Identity `json:"parent,omitempty"`
	Repository *runs.Repository   `json:"repository,omitempty"`
}
type Mapping struct {
	Path []string `json:"path"`
	To   string   `json:"to"`
}
type Problem struct {
	Repository    string   `json:"repository"`
	Target        string   `json:"target"`
	Rule          string   `json:"rule"`
	IdentityPath  []string `json:"identity_path"`
	WindowSeconds int      `json:"window_seconds"`
	Mode          string   `json:"mode"`
}
type Definition struct {
	Name           string           `json:"name"`
	Targets        []Target         `json:"targets"`
	Condition      Condition        `json:"condition"`
	Action         Action           `json:"action"`
	Mode           string           `json:"mode"`
	Bounds         Bounds           `json:"bounds"`
	Problem        *Problem         `json:"problem,omitempty"`
	IncludeDerived bool             `json:"include_derived"`
	Required       []contexts.Fixed `json:"required"`
}

func Template() Definition {
	return Definition{Mode: "auto", Action: Action{Kind: "run", Start: true, Mappings: []Mapping{}}, Bounds: Bounds{Depth: 3, Publications: 10, Evaluations: 100, Seconds: 86400, PerEvaluation: 1}, Problem: &Problem{Mode: "link_open", WindowSeconds: 86400}, Targets: []Target{}, Required: []contexts.Fixed{}}
}

type Activation struct {
	Definition Ref    `json:"definition"`
	Enabled    bool   `json:"enabled"`
	Controller string `json:"controller"`
}
type Source struct {
	Event  string   `json:"event"`
	Target Ref      `json:"target"`
	Kind   string   `json:"kind"`
	Keys   []string `json:"keys"`
	Facts  Facts    `json:"facts"`
	Refs   []Ref    `json:"refs"`
	Root   string   `json:"root,omitempty"`
	Depth  int      `json:"depth"`
}
type Evaluation struct {
	Cost        *usage.CostState `json:"cost,omitempty"`
	Definition  Ref              `json:"definition"`
	Source      Source           `json:"source"`
	Root        string           `json:"root"`
	Depth       int              `json:"depth"`
	State       string           `json:"state"`
	Reason      string           `json:"reason"`
	Verdict     string           `json:"verdict"`
	DryRun      bool             `json:"dry_run"`
	Destination *Ref             `json:"destination,omitempty"`
	At          time.Time        `json:"at"`
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
type Request struct {
	Mode    string `json:"mode"`
	Kind    string `json:"kind,omitempty"`
	ID      string `json:"id,omitempty"`
	Root    string `json:"root,omitempty"`
	After   int64  `json:"after,omitempty"`
	AfterID string `json:"after_id,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}
type Page struct {
	Items   []Record `json:"items"`
	After   int64    `json:"after"`
	AfterID string   `json:"after_id"`
}
type Past struct {
	apicontract.Mutation
	Definition Ref    `json:"definition"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	After      int64  `json:"after"`
	Limit      int    `json:"limit"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "0" && r.Version != "head"
}
