// Package graphindex owns ArcadeDB projections of PostgreSQL relationships.
package graphindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

type Limits struct {
	Depth, Nodes, Edges, Page int
	Timeout                   time.Duration
}

func DefaultLimits() Limits { return Limits{8, 2000, 10000, 100, 2 * time.Second} }
func EnvironmentLimits() (Limits, error) {
	l := DefaultLimits()
	for name, target := range map[string]*int{"KOUJOU_GRAPH_DEPTH": &l.Depth, "KOUJOU_GRAPH_NODES": &l.Nodes, "KOUJOU_GRAPH_EDGES": &l.Edges, "KOUJOU_GRAPH_PAGE": &l.Page} {
		if raw, ok := os.LookupEnv(name); ok {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				return l, errors.New("invalid graph limits")
			}
			*target = n
		}
	}
	if raw, ok := os.LookupEnv("KOUJOU_GRAPH_TIMEOUT_MS"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return l, errors.New("invalid graph timeout")
		}
		l.Timeout = time.Duration(n) * time.Millisecond
	}
	return l, nil
}

type Node struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision string `json:"revision,omitempty"`
	Form     string `json:"form"`
	Current  bool   `json:"current"`
	Deleted  bool   `json:"deleted"`
}

func (n Node) Ref() contexts.Ref { return contexts.Ref{Kind: n.Kind, ID: n.ID, Revision: n.Revision} }

type Relation struct {
	Kind    string           `json:"kind"`
	From    contexts.Ref     `json:"from"`
	To      contexts.Ref     `json:"to"`
	Order   int              `json:"order"`
	Present bool             `json:"present"`
	Origin  *contexts.Origin `json:"origin,omitempty"`
}
type RelationWrite struct {
	OperationID     string   `json:"operation_id"`
	ExpectedVersion string   `json:"expected_version"`
	Value           Relation `json:"value"`
}
type Edge struct {
	ID     string           `json:"id"`
	Kind   string           `json:"kind"`
	From   contexts.Ref     `json:"from"`
	To     contexts.Ref     `json:"to"`
	Order  int              `json:"order"`
	Source contexts.Ref     `json:"source"`
	Origin *contexts.Origin `json:"origin,omitempty"`
}
type Request struct {
	Root      contexts.Ref `json:"root"`
	Direction string       `json:"direction"`
	Kinds     []string     `json:"kinds,omitempty"`
	History   bool         `json:"history,omitempty"`
	Depth     int          `json:"depth,omitempty"`
	Nodes     int          `json:"nodes,omitempty"`
	Edges     int          `json:"edges,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	Cursor    string       `json:"cursor,omitempty"`
}
type Item struct {
	Node *Node `json:"node,omitempty"`
	Edge *Edge `json:"edge,omitempty"`
}
type Result struct {
	Items      []Item                     `json:"items"`
	Unexplored []string                   `json:"unexplored"`
	Evidence   contexts.CandidateEvidence `json:"evidence"`
}
type checkpoint struct {
	Sequence int64
	Position string
}
type vertex struct {
	G       string `json:"g"`
	K       string `json:"k"`
	W       string `json:"w"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Rev     string `json:"rev"`
	Form    string `json:"form"`
	Current bool   `json:"current"`
	Deleted bool   `json:"deleted"`
	Logical string `json:"logical"`
}
type arcEdge struct {
	G      string           `json:"g"`
	K      string           `json:"k"`
	W      string           `json:"w"`
	F      string           `json:"f"`
	T      string           `json:"t"`
	Kind   string           `json:"kind"`
	Ord    int              `json:"ord"`
	Owner  string           `json:"owner"`
	Source string           `json:"source"`
	Origin *contexts.Origin `json:"-"`
}

func encode(v any) []byte                      { b, _ := json.Marshal(v); return b }
func hash(v any) string                        { sum := sha256.Sum256(encode(v)); return hex.EncodeToString(sum[:]) }
func key(w string, r contexts.Ref) string      { return hash([]string{w, r.Kind, r.ID, r.Revision}) }
func identity(w string, r contexts.Ref) string { r.Revision = ""; return key(w, r) }
func unitKind(k string) bool {
	return k == "sentence" || k == "paragraph" || k == "section" || k == "document"
}
func orchestrationKind(k string) bool {
	return k == "hopper-result" || k == "publication" || k == "workflow-execution"
}
func resource(r contexts.Ref) string {
	if unitKind(r.Kind) {
		return "context:" + r.ID
	}
	return r.Kind + ":" + r.ID
}
func edgeKind(k string) bool {
	switch k {
	case "contains", "membership", "version", "scope_parent", "reference", "uses", "generated", "semantic":
		return true
	}
	return false
}
