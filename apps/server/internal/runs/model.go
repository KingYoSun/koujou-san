// Package runs owns accepted execution records, not a process launcher.
package runs

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/google/jsonschema-go/jsonschema"
)

var ErrInvalid = errors.New("invalid_run")
var ErrState = errors.New("run_state_conflict")

type Ref = access.VersionRef

// Target is the declared command/connection, never an inferred tool or a secret
// read from the device. Binding declares how optional inputs are delivered.
type Command struct {
	Kind       string            `json:"kind"`
	Target     string            `json:"target"`
	Parameters json.RawMessage   `json:"parameters,omitempty"`
	Binding    map[string]string `json:"binding,omitempty"`
}
type Input struct {
	Context json.RawMessage `json:"context,omitempty"`
	Prompt  json.RawMessage `json:"prompt,omitempty"`
	Command Command         `json:"command"`
}
type Contract struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	ResultSchema json.RawMessage `json:"result_schema,omitempty"`
	VerdictField string          `json:"verdict_field,omitempty"`
}
type Spec struct {
	Name       string           `json:"name,omitempty"`
	Input      Input            `json:"input"`
	Manifest   *contexts.Ref    `json:"manifest,omitempty"`
	Derivation *Contract        `json:"derivation,omitempty"`
	Previous   *Ref             `json:"previous,omitempty"`
	Sources    []Ref            `json:"sources,omitempty"`
	Artifacts  []contexts.Fixed `json:"artifacts,omitempty"`
}
type Assessment struct {
	State   string          `json:"state"`
	Verdict string          `json:"verdict,omitempty"`
	Report  json.RawMessage `json:"report,omitempty"`
}
type Link struct {
	Previous Ref `json:"previous"`
	Next     Ref `json:"next"`
}
type ExecutionInfo struct {
	Environment     string `json:"environment"`
	Version         string `json:"version"`
	VersionObserved bool   `json:"version_observed"`
	Pause           bool   `json:"pause"`
	Stop            bool   `json:"stop"`
}
type Run struct {
	Execution            *ExecutionInfo `json:"execution,omitempty"`
	Assignment           *Assignment    `json:"assignment,omitempty"`
	Preparation          string         `json:"preparation,omitempty"`
	Name                 string         `json:"name,omitempty"`
	Input                Ref            `json:"input"`
	Previous             *Ref           `json:"previous,omitempty"`
	RequestedBy          string         `json:"requested_by"`
	CreatedAt            time.Time      `json:"created_at"`
	ExecutionRequestedAt *time.Time     `json:"execution_requested_at,omitempty"`
	StartedAt            *time.Time     `json:"started_at,omitempty"`
	FinishedAt           *time.Time     `json:"finished_at,omitempty"`
	State                string         `json:"state"`
	Observation          string         `json:"observation"`
	ReportedThrough      string         `json:"reported_through"`
	CancelRequested      bool           `json:"cancel_requested"`
	ActivePause          *Ref           `json:"active_pause,omitempty"`
	ExitCode             *int           `json:"exit_code,omitempty"`
	Assessment           *Assessment    `json:"assessment,omitempty"`
}
type PauseRequest struct {
	ID           string          `json:"id"`
	Reason       string          `json:"reason"`
	Request      json.RawMessage `json:"request"`
	Condition    json.RawMessage `json:"condition,omitempty"`
	ResponseKind string          `json:"response_kind,omitempty"`
}
type ResponseOrigin struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
}
type Pause struct {
	PauseRequest
	State          string          `json:"state"`
	CreatedAt      time.Time       `json:"created_at"`
	ResponseID     string          `json:"response_id,omitempty"`
	Response       json.RawMessage `json:"response,omitempty"`
	AnsweredBy     string          `json:"answered_by,omitempty"`
	AnsweredAt     *time.Time      `json:"answered_at,omitempty"`
	DeliveredAt    *time.Time      `json:"delivered_at,omitempty"`
	ResumedAt      *time.Time      `json:"resumed_at,omitempty"`
	ResponseOrigin *ResponseOrigin `json:"response_origin,omitempty"`
}
type Output struct {
	Sequence  string           `json:"sequence"`
	Channel   string           `json:"channel"`
	Source    string           `json:"source"`
	Value     json.RawMessage  `json:"value,omitempty"`
	Artifacts []contexts.Fixed `json:"artifacts,omitempty"`
	At        time.Time        `json:"at"`
}
type Record struct {
	Ref
	RunID    string          `json:"run_id"`
	Kind     string          `json:"kind"`
	Position string          `json:"position"`
	Value    json.RawMessage `json:"value"`
	Refs     []Ref           `json:"refs"`
}
type Create struct {
	apicontract.Mutation
	Spec Spec `json:"spec"`
}
type Answer struct {
	apicontract.Mutation
	PauseID string          `json:"pause_id"`
	Value   json.RawMessage `json:"value"`
	Sources []Ref           `json:"sources,omitempty"`
}

// Event is reported by the assigned authenticated execution adapter. Receiving a
// response, delivering it and confirming continuation are separate events.
type Event struct {
	Execution    *ExecutionInfo `json:"execution,omitempty"`
	Generation   string         `json:"generation,omitempty"`
	PreparedArea *Area          `json:"prepared_area,omitempty"`
	apicontract.Mutation
	Sequence   string           `json:"sequence"`
	Type       string           `json:"type"`
	Pause      *PauseRequest    `json:"pause,omitempty"`
	PauseID    string           `json:"pause_id,omitempty"`
	ResponseID string           `json:"response_id,omitempty"`
	Channel    string           `json:"channel,omitempty"`
	Source     string           `json:"source,omitempty"`
	Value      json.RawMessage  `json:"value,omitempty"`
	ExitCode   *int             `json:"exit_code,omitempty"`
	Report     json.RawMessage  `json:"report,omitempty"`
	Sources    []Ref            `json:"sources,omitempty"`
	Artifacts  []contexts.Fixed `json:"artifacts,omitempty"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "0" && r.Version != "head"
}
func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}
func validate(s Spec) error {
	c := s.Input.Command
	if strings.TrimSpace(c.Kind) == "" || strings.TrimSpace(c.Target) == "" {
		return ErrInvalid
	}
	for name, present := range map[string]bool{"context": len(s.Input.Context) > 0, "prompt": len(s.Input.Prompt) > 0} {
		if present && strings.TrimSpace(c.Binding[name]) == "" {
			return ErrInvalid
		}
	}
	if s.Manifest != nil && (len(s.Input.Context) > 0 || s.Manifest.Kind != "manifest" || s.Manifest.Revision == "" || s.Manifest.Revision == "head") {
		return ErrInvalid
	}
	if s.Previous != nil && !fixed(*s.Previous) {
		return ErrInvalid
	}
	for _, r := range s.Sources {
		if !fixed(r) {
			return ErrInvalid
		}
	}
	if s.Derivation != nil {
		if s.Derivation.Name == "" || s.Derivation.Version == "" {
			return ErrInvalid
		}
		if len(s.Derivation.ResultSchema) > 0 {
			if _, err := schema(s.Derivation.ResultSchema); err != nil {
				return ErrInvalid
			}
		}
	}
	return nil
}
func schema(b json.RawMessage) (*jsonschema.Resolved, error) {
	var s jsonschema.Schema
	if json.Unmarshal(b, &s) != nil {
		return nil, ErrInvalid
	}
	// No Loader: external references cannot fetch schemas or perform network I/O.
	return s.Resolve(nil)
}
func assess(c *Contract, report json.RawMessage) *Assessment {
	if c == nil {
		return nil
	}
	a := &Assessment{State: "valid", Report: report}
	var value any
	if len(c.ResultSchema) > 0 {
		s, err := schema(c.ResultSchema)
		if err != nil || json.Unmarshal(report, &value) != nil || s.Validate(value) != nil {
			a.State = "invalid"
			return a
		}
	}
	if c.VerdictField != "" {
		var object map[string]json.RawMessage
		if json.Unmarshal(report, &object) != nil || json.Unmarshal(object[c.VerdictField], &a.Verdict) != nil || (a.Verdict != "PASS" && a.Verdict != "FAIL" && a.Verdict != "INCONCLUSIVE") {
			a.State = "invalid"
			a.Verdict = ""
		}
	}
	return a
}
