// Package searchindex owns the rebuildable Bleve projection. PostgreSQL remains
// authoritative for content, versions, direct membership and authorization.
package searchindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

const schema = "koujou-search-v1-cjk-bleve-2.6.1"
const pageSize = 100

type Request struct {
	Text      string              `json:"text,omitempty"`
	Kinds     []string            `json:"kinds,omitempty"`
	IDs       []string            `json:"ids,omitempty"`
	Revisions []string            `json:"revisions,omitempty"`
	Scopes    []contexts.Identity `json:"scopes,omitempty"`
	Tags      []string            `json:"tags,omitempty"`
	Limit     int                 `json:"limit"`
	Cursor    string              `json:"cursor,omitempty"`
}
type Result struct {
	Items    []contexts.Record          `json:"items"`
	Count    int                        `json:"count"`
	Evidence contexts.CandidateEvidence `json:"evidence"`
}
type document struct {
	Workspace string   `json:"workspace"`
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Revision  string   `json:"revision"`
	Current   bool     `json:"current"`
	Text      string   `json:"text"`
	Scopes    []string `json:"scopes"`
	Tags      []string `json:"tags"`
	Source    string   `json:"source"`
}
type source struct {
	Ref          contexts.Ref   `json:"ref"`
	Dependencies []contexts.Ref `json:"dependencies"`
}
type checkpoint struct {
	Sequence int64
	Position string
}
type generation struct{ Schema, Name string }
type cursor struct {
	User, Workspace, Query, Generation, Position string
	After                                        []string
}

func encode(v any) []byte { raw, _ := json.Marshal(v); return raw }
func hash(v any) string   { sum := sha256.Sum256(encode(v)); return hex.EncodeToString(sum[:]) }
func docID(w string, r contexts.Ref) string {
	return string(encode([]string{w, r.Kind, r.ID, r.Revision}))
}
func scopeID(r contexts.Identity) string { return string(encode(r)) }
func unitKind(k string) bool {
	return k == "sentence" || k == "paragraph" || k == "section" || k == "document"
}
func checkpointKey(w string) []byte { return []byte("workspace:" + w) }
func readCheckpoint(i bleve.Index, w string) (checkpoint, error) {
	var cp checkpoint
	raw, err := i.GetInternal(checkpointKey(w))
	if err != nil {
		return cp, err
	}
	if len(raw) > 0 {
		err = json.Unmarshal(raw, &cp)
	}
	return cp, err
}
func indexMapping() mapping.IndexMapping {
	m := bleve.NewIndexMapping()
	m.DefaultAnalyzer = "cjk"
	d := bleve.NewDocumentMapping()
	d.Dynamic = false
	for _, name := range []string{"workspace", "id", "kind", "revision", "scopes", "tags"} {
		f := bleve.NewKeywordFieldMapping()
		f.IncludeInAll = false
		f.DocValues = true
		d.AddFieldMappingsAt(name, f)
	}
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "cjk"
	text.Store = false
	text.IncludeInAll = false
	d.AddFieldMappingsAt("text", text)
	current := bleve.NewBooleanFieldMapping()
	current.IncludeInAll = false
	d.AddFieldMappingsAt("current", current)
	src := bleve.NewTextFieldMapping()
	src.Index = false
	src.Store = true
	src.IncludeInAll = false
	d.AddFieldMappingsAt("source", src)
	m.DefaultMapping = d
	return m
}
func term(field, value string) query.Query {
	q := bleve.NewTermQuery(value)
	q.SetField(field)
	return q
}
func makeQuery(w string, r Request) (query.Query, error) {
	if r.Limit < 1 || r.Limit > pageSize {
		return nil, fmt.Errorf("limit must be between 1 and %d", pageSize)
	}
	parts := []query.Query{term("workspace", w)}
	for field, values := range map[string][]string{"kind": r.Kinds, "id": r.IDs, "revision": r.Revisions} {
		if len(values) > 0 {
			alternatives := []query.Query{}
			for _, v := range values {
				if v == "" || (field == "kind" && !unitKind(v)) {
					return nil, fmt.Errorf("invalid filter")
				}
				alternatives = append(alternatives, term(field, v))
			}
			parts = append(parts, bleve.NewDisjunctionQuery(alternatives...))
		}
	}
	if len(r.Revisions) == 0 {
		q := bleve.NewBoolFieldQuery(true)
		q.SetField("current")
		parts = append(parts, q)
	}
	for _, v := range r.Scopes {
		if v.ID == "" || !(v.Kind == "product" || v.Kind == "project" || v.Kind == "milestone" || v.Kind == "issue") {
			return nil, fmt.Errorf("invalid scope")
		}
		parts = append(parts, term("scopes", scopeID(v)))
	}
	for _, v := range r.Tags {
		parts = append(parts, term("tags", v))
	}
	if r.Text != "" {
		q := bleve.NewMatchQuery(r.Text)
		q.SetField("text")
		q.SetOperator(query.MatchQueryOperatorAnd)
		parts = append(parts, q)
	}
	return bleve.NewConjunctionQuery(parts...), nil
}
