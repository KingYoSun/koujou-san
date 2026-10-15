// Package contexts owns immutable content, grouping and resolved input records.
package contexts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Identity struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Ref struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

func (r Ref) Identity() Identity { return Identity{r.Kind, r.ID} }
func (r Ref) key() string        { return r.Kind + ":" + r.ID + "@" + r.Revision }

type Origin struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Revision    string `json:"revision"`
	Digest      string `json:"digest"`
	Path        string `json:"path,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

// Fixed has an explicit delivery mode. A reference does not claim to contain its
// source's body; a snapshot must match the digest of its canonical JSON value.
type Fixed struct {
	URI       string          `json:"uri"`
	Revision  string          `json:"revision"`
	Range     string          `json:"range"`
	Digest    string          `json:"digest"`
	Delivery  string          `json:"delivery"`
	State     string          `json:"state"`
	Structure string          `json:"structure,omitempty"`
	Snapshot  json.RawMessage `json:"snapshot,omitempty"`
}
type Unit struct {
	Kind        string  `json:"kind"`
	Name        string  `json:"name,omitempty"`
	Text        *string `json:"text,omitempty"`
	Title       string  `json:"title,omitempty"`
	Children    []Ref   `json:"children,omitempty"`
	Attachments []Fixed `json:"attachments,omitempty"`
	External    *Fixed  `json:"external,omitempty"`
	Origin      *Origin `json:"origin,omitempty"`
}
type Scope struct {
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Parent *Identity `json:"parent,omitempty"`
}
type Membership struct {
	Scope   Identity `json:"scope"`
	Context Identity `json:"context"`
	Present bool     `json:"present"`
}
type Direct struct {
	Value  json.RawMessage `json:"value,omitempty"`
	Origin *Origin         `json:"origin,omitempty"`
	State  string          `json:"state,omitempty"`
}
type Selection struct {
	Mode     string   `json:"mode,omitempty"`
	Include  []Ref    `json:"include,omitempty"`
	Exclude  []Ref    `json:"exclude,omitempty"`
	Direct   []Direct `json:"direct,omitempty"`
	External []Fixed  `json:"external,omitempty"`
}
type PlannedInput struct {
	RunID     string    `json:"run_id"`
	Selection Selection `json:"selection"`
}
type Baseline struct {
	Contexts       []Ref          `json:"contexts"`
	Memberships    []Ref          `json:"memberships"`
	Inputs         []PlannedInput `json:"inputs,omitempty"`
	Previous       *Ref           `json:"previous,omitempty"`
	Changed        []Identity     `json:"changed,omitempty"`
	AffectedRunIDs []string       `json:"affected_run_ids,omitempty"`
}
type Placement struct {
	Ref     Ref   `json:"ref"`
	Path    []int `json:"path"`
	Excerpt bool  `json:"excerpt,omitempty"`
}
type Gap struct {
	Code   string `json:"code"`
	Ref    *Ref   `json:"ref,omitempty"`
	Source string `json:"source,omitempty"`
}
type Manifest struct {
	Schema      string      `json:"schema"`
	Assembler   string      `json:"assembler"`
	WorkspaceID string      `json:"workspace_id"`
	RunID       string      `json:"run_id"`
	Baseline    *Ref        `json:"baseline,omitempty"`
	Memberships []Ref       `json:"memberships,omitempty"`
	Selection   Selection   `json:"selection"`
	OperationID string      `json:"operation_id"`
	AdoptedBy   string      `json:"adopted_by"`
	State       string      `json:"state"`
	InputUTF8   *string     `json:"input_utf8,omitempty"`
	Digest      string      `json:"digest"`
	Placements  []Placement `json:"placements"`
	Excluded    []Ref       `json:"excluded"`
	Duplicates  []Ref       `json:"duplicates"`
	ReadStatus  string      `json:"read_status"`
}
type Resolution struct {
	Ready    bool      `json:"ready"`
	Missing  []Gap     `json:"missing"`
	Manifest *Manifest `json:"manifest,omitempty"`
}
type ResolveRequest struct {
	RunID     string    `json:"run_id"`
	Baseline  *Ref      `json:"baseline,omitempty"`
	Selection Selection `json:"selection"`
	// CandidateEvidence carries the projection boundary; it never supplies content
	// or authorization. Its absence means explicit-reference resolution only.
	Evidence []CandidateEvidence `json:"evidence,omitempty"`
}
type CandidateEvidence struct {
	Engine           string `json:"engine"`
	Position         string `json:"position"`
	RequiredPosition string `json:"required_position"`
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
}
type Addition struct {
	Manifest   Ref    `json:"manifest"`
	PauseID    string `json:"pause_id"`
	ResponseID string `json:"response_id"`
	Context    Direct `json:"context"`
	ReadStatus string `json:"read_status"`
}
type failure struct{ gap Gap }

func (f *failure) Error() string        { return f.gap.Code }
func invalid(code string, r *Ref) error { return &failure{Gap{Code: code, Ref: r}} }
func canonical(value any) []byte        { b, _ := json.Marshal(value); return b }
func digest(b []byte) string            { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func canonicalRaw(raw json.RawMessage) (json.RawMessage, error) {
	var compact any
	if !json.Valid(raw) {
		return nil, invalid("invalid_value", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&compact); err != nil {
		return nil, invalid("invalid_value", nil)
	}
	return canonical(compact), nil
}
func unitLevel(kind string) int {
	switch kind {
	case "sentence":
		return 1
	case "paragraph":
		return 2
	case "section":
		return 3
	case "document":
		return 4
	}
	return 0
}
func scopeLevel(kind string) int {
	switch kind {
	case "product":
		return 1
	case "project":
		return 2
	case "milestone":
		return 3
	case "issue":
		return 4
	}
	return 0
}
func validRef(r Ref) bool {
	return r.Kind != "" && r.ID != "" && r.Revision != "" && r.Revision != "head"
}
func validateUnit(u Unit) error {
	level := unitLevel(u.Kind)
	if level == 0 {
		return invalid("invalid_type", nil)
	}
	if level == 1 {
		if (u.Text == nil) == (u.External == nil) || len(u.Children) > 0 || u.Title != "" {
			return invalid("invalid_structure", nil)
		}
		if u.External != nil && (u.External.Structure != "sentence" || u.External.Delivery != "snapshot") {
			return invalid("unstructured_external", nil)
		}
	} else if u.Text != nil || u.External != nil || (level == 2 && u.Title != "") {
		return invalid("invalid_structure", nil)
	}
	for _, r := range u.Children {
		if !validRef(r) || unitLevel(r.Kind) != level-1 {
			return invalid("invalid_type", &r)
		}
	}
	for _, f := range u.Attachments {
		if err := validateFixed(f); err != nil {
			return err
		}
	}
	if u.External != nil {
		if err := validateFixed(*u.External); err != nil {
			return err
		}
	}
	return validateOrigin(u.Origin)
}
func validateOrigin(o *Origin) error {
	if o != nil && (o.Kind == "" || o.ID == "" || o.Revision == "" || o.Digest == "") {
		return invalid("invalid_origin", nil)
	}
	return nil
}
func validateFixed(f Fixed) error {
	if f.URI == "" || f.Revision == "" || f.Range == "" || len(f.Digest) != 64 || (f.Delivery != "reference" && f.Delivery != "snapshot") || (f.State != "ready" && f.State != "unfetched" && f.State != "failed") {
		return invalid("invalid_external", nil)
	}
	if _, err := hex.DecodeString(f.Digest); err != nil {
		return invalid("invalid_external", nil)
	}
	if f.Delivery == "reference" {
		if len(f.Snapshot) > 0 {
			return invalid("invalid_external", nil)
		}
		return nil
	}
	if f.State == "ready" {
		value, err := canonicalRaw(f.Snapshot)
		if err != nil {
			return err
		}
		if digest(value) != f.Digest {
			return invalid("digest_mismatch", nil)
		}
	}
	return nil
}
func fixedValue(f Fixed) (json.RawMessage, error) {
	if err := validateFixed(f); err != nil {
		return nil, err
	}
	if f.State != "ready" {
		return nil, &failure{Gap{Code: "external_" + f.State, Source: f.URI}}
	}
	if f.Delivery == "snapshot" {
		return canonicalRaw(f.Snapshot)
	}
	return canonical(map[string]string{"uri": f.URI, "revision": f.Revision, "range": f.Range, "digest": f.Digest}), nil
}
func directValue(d Direct) (json.RawMessage, error) {
	if err := validateOrigin(d.Origin); err != nil {
		return nil, err
	}
	if d.State != "" && d.State != "ready" {
		source := ""
		if d.Origin != nil {
			source = d.Origin.ID
		}
		return nil, &failure{Gap{Code: "source_unavailable", Source: source}}
	}
	value, err := canonicalRaw(d.Value)
	if err != nil {
		return nil, err
	}
	// The origin digest identifies the source before extraction. The final input
	// digest covers this selected value independently.
	return value, nil
}
