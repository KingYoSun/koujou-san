package contexts

import (
	"context"
	"encoding/json"
	"errors"
)

type Limits struct {
	Bytes      int
	Placements int
}

func DefaultLimits() Limits { return Limits{16 * 1024 * 1024, 100000} }

type Loader func(context.Context, Ref) (Unit, error)
type inputNode struct {
	Kind        string            `json:"kind"`
	Title       string            `json:"title,omitempty"`
	Text        *string           `json:"text,omitempty"`
	Children    []*inputNode      `json:"children,omitempty"`
	Attachments []json.RawMessage `json:"attachments,omitempty"`
	Excerpt     bool              `json:"excerpt,omitempty"`
}
type inputValue struct {
	Contexts []*inputNode      `json:"contexts"`
	Direct   []json.RawMessage `json:"direct,omitempty"`
	External []json.RawMessage `json:"external,omitempty"`
}
type graph struct {
	ctx      context.Context
	loader   Loader
	units    map[string]Unit
	visiting map[string]bool
}

func (g *graph) load(r Ref) error {
	if !validRef(r) || unitLevel(r.Kind) == 0 {
		return invalid("invalid_reference", &r)
	}
	if g.visiting[r.key()] {
		return invalid("cycle", &r)
	}
	if _, ok := g.units[r.key()]; ok {
		return nil
	}
	g.visiting[r.key()] = true
	defer delete(g.visiting, r.key())
	u, err := g.loader(g.ctx, r)
	if err != nil {
		return err
	}
	if u.Kind != r.Kind {
		return invalid("invalid_type", &r)
	}
	if err = validateUnit(u); err != nil {
		return err
	}
	for _, child := range u.Children {
		if err = g.load(child); err != nil {
			return err
		}
	}
	g.units[r.key()] = u
	return nil
}
func (g *graph) descendants(r Ref, visit func(Ref)) {
	seen := map[string]bool{}
	var walk func(Ref)
	walk = func(ref Ref) {
		if seen[ref.key()] {
			return
		}
		seen[ref.key()] = true
		visit(ref)
		for _, c := range g.units[ref.key()].Children {
			walk(c)
		}
	}
	walk(r)
}
func unresolved(err error) Resolution {
	var f *failure
	if !errors.As(err, &f) {
		f = &failure{Gap{Code: "unavailable"}}
	}
	return Resolution{Missing: []Gap{f.gap}}
}

// Resolve builds only the explicitly selected input. Metadata remains in the
// manifest, and a failure never produces a complete or startable manifest.
func Resolve(ctx context.Context, load Loader, w string, request ResolveRequest, limits Limits) Resolution {
	s := request.Selection
	m := &Manifest{Schema: "context-manifest-v1", Assembler: "structured-json-v1", WorkspaceID: w, RunID: request.RunID, Baseline: request.Baseline, Selection: s, State: "ready", ReadStatus: "unobserved", Placements: []Placement{}, Excluded: []Ref{}, Duplicates: []Ref{}}
	if limits.Bytes <= 0 || limits.Placements <= 0 {
		return unresolved(invalid("invalid_limits", nil))
	}
	hasInput := len(s.Include)+len(s.Exclude)+len(s.Direct)+len(s.External) > 0
	if s.Mode != "" && s.Mode != "none" && s.Mode != "select" {
		return unresolved(invalid("invalid_selection", nil))
	}
	if s.Mode == "none" && hasInput {
		return unresolved(invalid("selection_conflict", nil))
	}
	if s.Mode == "" && hasInput {
		return unresolved(invalid("selection_required", nil))
	}
	if s.Mode == "none" || s.Mode == "" {
		m.State = "none"
		m.Digest = digest(nil)
		return Resolution{Ready: true, Missing: []Gap{}, Manifest: m}
	}
	for _, e := range request.Evidence {
		if e.Engine == "" || e.State != "complete" || e.Position == "" || e.Position != e.RequiredPosition {
			return Resolution{Missing: []Gap{{Code: "incomplete_candidates", Source: e.Engine + ":" + e.Reason}}}
		}
	}
	g := graph{ctx, load, map[string]Unit{}, map[string]bool{}}
	for _, r := range append(append([]Ref{}, s.Include...), s.Exclude...) {
		if err := g.load(r); err != nil {
			return unresolved(err)
		}
	}
	excluded := map[string]bool{}
	for _, r := range s.Exclude {
		g.descendants(r, func(ref Ref) { excluded[ref.Kind+":"+ref.ID] = true })
	}
	// Direct duplicates are removed, then descendants covered by another selected
	// ancestor are removed. A different revision remains and is checked below.
	roots := []Ref{}
	selected := map[string]bool{}
	for _, r := range s.Include {
		if selected[r.key()] {
			m.Duplicates = append(m.Duplicates, r)
			continue
		}
		selected[r.key()] = true
		roots = append(roots, r)
	}
	covered := map[string]bool{}
	for _, r := range roots {
		g.descendants(r, func(ref Ref) {
			if ref.key() != r.key() {
				covered[ref.key()] = true
			}
		})
	}
	versions := map[Identity]string{}
	excludedSeen := map[Identity]bool{}
	var build func(Ref, []int) (*inputNode, error)
	count := 0
	build = func(r Ref, path []int) (*inputNode, error) {
		if excluded[r.Kind+":"+r.ID] {
			if !excludedSeen[r.Identity()] {
				m.Excluded = append(m.Excluded, r)
				excludedSeen[r.Identity()] = true
			}
			return nil, nil
		}
		if old, ok := versions[r.Identity()]; ok && old != r.Revision {
			return nil, invalid("conflicting_revisions", &r)
		}
		versions[r.Identity()] = r.Revision
		count++
		if count > limits.Placements {
			return nil, invalid("placement_limit", &r)
		}
		u := g.units[r.key()]
		node := &inputNode{Kind: u.Kind, Title: u.Title, Text: u.Text}
		start := len(m.Placements)
		m.Placements = append(m.Placements, Placement{Ref: r, Path: append([]int{}, path...)})
		if u.External != nil {
			value, err := fixedValue(*u.External)
			if err != nil {
				return nil, err
			}
			var text string
			if json.Unmarshal(value, &text) != nil {
				return nil, invalid("invalid_external_sentence", &r)
			}
			node.Text = &text
		}
		for _, f := range u.Attachments {
			value, err := fixedValue(f)
			if err != nil {
				return nil, err
			}
			node.Attachments = append(node.Attachments, value)
		}
		for _, c := range u.Children {
			child, err := build(c, append(append([]int{}, path...), len(node.Children)))
			if err != nil {
				return nil, err
			}
			if child == nil {
				node.Excerpt = true
			} else {
				node.Children = append(node.Children, child)
				node.Excerpt = node.Excerpt || child.Excerpt
			}
		}
		if unitLevel(u.Kind) > 1 && len(node.Children) == 0 && len(node.Attachments) == 0 {
			m.Placements = m.Placements[:start]
			return nil, nil
		}
		m.Placements[start].Excerpt = node.Excerpt
		return node, nil
	}
	input := inputValue{Contexts: []*inputNode{}}
	for _, r := range roots {
		if covered[r.key()] {
			m.Duplicates = append(m.Duplicates, r)
			continue
		}
		node, err := build(r, []int{len(input.Contexts)})
		if err != nil {
			return unresolved(err)
		}
		if node != nil {
			input.Contexts = append(input.Contexts, node)
		}
	}
	for _, d := range s.Direct {
		value, err := directValue(d)
		if err != nil {
			return unresolved(err)
		}
		input.Direct = append(input.Direct, value)
	}
	for _, f := range s.External {
		value, err := fixedValue(f)
		if err != nil {
			return unresolved(err)
		}
		input.External = append(input.External, value)
	}
	encoded := canonical(input)
	if len(encoded) > limits.Bytes {
		return unresolved(invalid("byte_limit", nil))
	}
	text := string(encoded)
	m.InputUTF8 = &text
	m.Digest = digest(encoded)
	return Resolution{Ready: true, Missing: []Gap{}, Manifest: m}
}
