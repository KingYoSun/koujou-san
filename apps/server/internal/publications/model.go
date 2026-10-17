// Package publications records issue requests and observations; GitHub owns the
// external issue. A durable send intent is never automatically sent twice.
package publications

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"strings"
	"time"
)

type Ref = access.VersionRef

var ErrInvalid = errors.New("invalid_publication")
var ErrState = errors.New("publication_state_conflict")

type Draft struct {
	Name       string `json:"name"`
	Connection string `json:"connection"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Sources    []Ref  `json:"sources"`
}
type Actor struct {
	Kind    string `json:"kind"`
	User    string `json:"user_id"`
	Control *Ref   `json:"control,omitempty"`
}
type Request struct {
	Accepted   Ref        `json:"accepted"`
	Connection Connection `json:"connection"`
	Draft      Ref        `json:"draft"`
	Actor      Actor      `json:"actor"`
	Input      Draft      `json:"input"`
	Marker     string     `json:"marker"`
	Body       string     `json:"external_body"`
	Run        string     `json:"run_id"`
	State      string     `json:"state"`
	Reason     string     `json:"reason"`
	External   *Issue     `json:"external,omitempty"`
	At         time.Time  `json:"accepted_at"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}
type Issue struct {
	ID          int64           `json:"id"`
	Number      int64           `json:"number"`
	URL         string          `json:"html_url"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request,omitempty"`
}
type Outcome struct {
	State  string    `json:"state"`
	Reason string    `json:"reason"`
	Issue  *Issue    `json:"issue,omitempty"`
	At     time.Time `json:"at"`
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
type Submit struct {
	apicontract.Mutation
	Draft Ref `json:"draft"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func fixed(r Ref) bool {
	return r.ID != "" && r.Version != "" && r.Version != "head" && r.Version != "0"
}
func marker(w, id string) string {
	sum := sha256.Sum256([]byte(w + "\x00" + id))
	return "<!-- koujou-san-request:" + hex.EncodeToString(sum[:]) + " -->"
}
func validDraft(d Draft) bool { return d.Connection != "" && strings.TrimSpace(d.Title) != "" }
