// Package clientops stores client-owned selection work without creating Runs.
package clientops

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

var ErrInvalid = errors.New("invalid_client_operation")
var ErrState = errors.New("client_operation_state_changed")

type Ref = access.VersionRef
type Record struct {
	Ref
	Kind         string          `json:"kind"`
	Root         string          `json:"root,omitempty"`
	Position     int64           `json:"position"`
	Value        json.RawMessage `json:"value"`
	Refs         []Ref           `json:"refs"`
	MetadataOnly bool            `json:"metadata_only,omitempty"`
	Dependencies []Ref           `json:"-"`
}
type Model struct {
	Cost       *usage.CostBudget    `json:"cost,omitempty"`
	Endpoint   string               `json:"endpoint,omitempty"`
	Connection string               `json:"connection"`
	Revision   string               `json:"revision"`
	Requested  string               `json:"requested"`
	Allowed    []string             `json:"allowed"`
	Contract   string               `json:"contract,omitempty"`
	Prices     map[string]usage.Ref `json:"prices,omitempty"`
	Valuation  string               `json:"valuation,omitempty"`
}
type Operation struct {
	AdoptPaths         []string         `json:"adopt_paths,omitempty"`
	Policy             Ref              `json:"policy"`
	Target             Ref              `json:"target"`
	Goal               string           `json:"goal"`
	Purpose            string           `json:"purpose"`
	Model              Model            `json:"model"`
	Client             string           `json:"client"`
	User               string           `json:"user"`
	State              string           `json:"state"`
	Reason             string           `json:"reason,omitempty"`
	ReadOperation      string           `json:"read_operation"`
	CandidateOperation string           `json:"candidate_operation"`
	Turn               *Ref             `json:"turn,omitempty"`
	Proposal           *Ref             `json:"proposal,omitempty"`
	Applied            *Ref             `json:"applied,omitempty"`
	StartedAt          *time.Time       `json:"started_at,omitempty"`
	Cost               *usage.CostState `json:"cost,omitempty"`
	EvidenceSince      *time.Time       `json:"evidence_since,omitempty"`
}
type Selection struct {
	Name      string             `json:"name"`
	Input     contexts.Selection `json:"input"`
	Manifest  contexts.Ref       `json:"manifest"`
	Operation *Ref               `json:"operation,omitempty"`
	Proposal  *Ref               `json:"proposal,omitempty"`
	Items     []Item             `json:"items"`
}

type Limits struct {
	Models  int               `json:"model_requests"`
	MCP     int               `json:"mcp_requests"`
	Seconds int               `json:"seconds"`
	Cost    *usage.CostBudget `json:"cost,omitempty"`
}
type Policy struct {
	Definition *DefinitionPolicy  `json:"definition,omitempty"`
	Mode       string             `json:"mode"`
	Limits     Limits             `json:"limits"`
	Fixed      contexts.Selection `json:"fixed"`
	Scopes     []string           `json:"scopes"`
}

func DefaultPolicy() Policy {
	return Policy{Mode: "auto", Limits: Limits{Models: 32, MCP: 256, Seconds: 600}, Fixed: contexts.Selection{Mode: "select"}, Scopes: []string{}}
}

type Item struct {
	Ref    contexts.Ref `json:"ref"`
	Reason string       `json:"reason"`
}
type Proposal struct {
	DefinitionUTF8 string   `json:"definition_utf8,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Items          []Item   `json:"items"`
	Questions      []string `json:"questions"`
	Unknown        []string `json:"unknown"`
}

func complete(p Proposal) bool { return len(p.Questions) == 0 && len(p.Unknown) == 0 }
func decodeProposal(b []byte, p *Proposal) error {
	if decode(b, p) != nil || p.Items == nil || p.Questions == nil || p.Unknown == nil {
		return ErrInvalid
	}
	for _, item := range p.Items {
		if !fixed(item.Ref) || item.Reason == "" {
			return ErrInvalid
		}
	}
	return nil
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func fixed(r contexts.Ref) bool {
	return (r.Kind == "sentence" || r.Kind == "paragraph" || r.Kind == "section" || r.Kind == "document") && r.ID != "" && r.Revision != "" && r.Revision != "head" && r.Revision != "0"
}

// selection keeps explicit manual choices and order. Context resolution checks
// descendants, exclusions, capacity and current authorization in the same tx.
func selection(locked contexts.Selection, p Proposal) (contexts.Selection, error) {
	if locked.Mode == "none" {
		if len(p.Items) != 0 {
			return contexts.Selection{}, ErrInvalid
		}
		return locked, nil
	}
	out := locked
	out.Mode = "select"
	out.Include = append([]contexts.Ref{}, locked.Include...)
	seen := map[contexts.Identity]string{}
	for _, r := range locked.Include {
		if !fixed(r) {
			return contexts.Selection{}, ErrInvalid
		}
		seen[r.Identity()] = r.Revision
	}
	for _, item := range p.Items {
		r := item.Ref
		if !fixed(r) || item.Reason == "" {
			return contexts.Selection{}, ErrInvalid
		}
		for _, excluded := range locked.Exclude {
			if excluded.Identity() == r.Identity() {
				return contexts.Selection{}, ErrInvalid
			}
		}
		if revision, ok := seen[r.Identity()]; ok {
			if revision != r.Revision {
				return contexts.Selection{}, ErrInvalid
			}
			continue
		}
		seen[r.Identity()] = r.Revision
		out.Include = append(out.Include, r)
	}
	if len(out.Include) == 0 && len(out.Direct) == 0 && len(out.External) == 0 {
		return contexts.Selection{}, ErrInvalid
	}
	return out, nil
}
