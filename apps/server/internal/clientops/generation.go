package clientops

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

type GenerationSource struct {
	Run    Ref                 `json:"run"`
	Output Ref                 `json:"output"`
	Path   []string            `json:"path"`
	Extras []GenerationExcerpt `json:"extras,omitempty"`
}
type GenerationExcerpt struct {
	Ref  Ref      `json:"ref"`
	Path []string `json:"path"`
}
type ContextLock struct {
	Ref    contexts.Ref      `json:"ref"`
	Fields map[string]string `json:"fields"`
}
type GenerationPolicy struct {
	Activation  *Ref             `json:"activation,omitempty"`
	Source      GenerationSource `json:"source"`
	Fixed       []ContextLock    `json:"fixed"`
	GroupScopes []contexts.Ref   `json:"group_scopes"`
	Tags        []string         `json:"tags,omitempty"`
}
type GenerationChange struct {
	Key        string         `json:"key"`
	Target     contexts.Ref   `json:"target"`
	ValueUTF8  string         `json:"value_utf8"`
	State      string         `json:"state"`
	Reason     string         `json:"reason"`
	References []contexts.Ref `json:"references"`
}
type GenerationGrouping struct {
	ID              string            `json:"id,omitempty"`
	Key             string            `json:"key"`
	Scope           contexts.Identity `json:"scope"`
	Context         contexts.Identity `json:"context"`
	Present         bool              `json:"present"`
	ExpectedVersion string            `json:"expected_version"`
}
type GenerationCandidate struct {
	Ref      contexts.Ref `json:"ref"`
	Relation string       `json:"relation"`
	Reason   string       `json:"reason"`
}
type GenerationProposal struct {
	Changes    []GenerationChange    `json:"changes"`
	Grouping   []GenerationGrouping  `json:"grouping"`
	Candidates []GenerationCandidate `json:"candidates"`
}
type GeneratedContext struct {
	Name       string         `json:"name"`
	Key        string         `json:"key"`
	Ref        contexts.Ref   `json:"ref"`
	State      string         `json:"state"`
	Reason     string         `json:"reason"`
	References []contexts.Ref `json:"references"`
}
type GenerationResult struct {
	Operation   Ref                   `json:"operation"`
	Source      GenerationSource      `json:"source"`
	Items       []GeneratedContext    `json:"items"`
	Memberships []contexts.Ref        `json:"memberships"`
	Candidates  []GenerationCandidate `json:"candidates"`
}

func generationFields(g *GenerationPolicy) bool {
	if g == nil || !pinned(g.Source.Run) || !pinned(g.Source.Output) || !strings.HasPrefix(g.Source.Run.ID, "run:") || !strings.HasPrefix(g.Source.Output.ID, "run-output:") {
		return false
	}
	if g.Activation != nil && (!pinned(*g.Activation) || !strings.HasPrefix(g.Activation.ID, "generation-active:")) {
		return false
	}
	for _, e := range g.Source.Extras {
		if !pinned(e.Ref) || (!strings.HasPrefix(e.Ref.ID, "run-output:") && !strings.HasPrefix(e.Ref.ID, "run-pause:")) {
			return false
		}
	}
	for _, f := range g.Fixed {
		if !fixed(f.Ref) {
			return false
		}
		for k, v := range f.Fields {
			if (k != "text" && k != "title" && k != "name" && k != "children") || (v != "" && !json.Valid([]byte(v))) {
				return false
			}
		}
	}
	for _, r := range g.GroupScopes {
		if r.Kind != "scope" || r.ID == "" || r.Revision == "" || r.Revision == "head" || r.Revision == "0" {
			return false
		}
	}
	return true
}
func generationRefs(g *GenerationPolicy) []Ref {
	if g == nil {
		return nil
	}
	out := []Ref{g.Source.Run, g.Source.Output}
	if g.Activation != nil {
		out = append(out, *g.Activation)
	}
	for _, e := range g.Source.Extras {
		out = append(out, e.Ref)
	}
	for _, f := range g.Fixed {
		out = append(out, Ref{ID: resource(f.Ref), Version: f.Ref.Revision})
	}
	for _, r := range g.GroupScopes {
		out = append(out, Ref{ID: "scope:" + r.ID, Version: r.Revision})
	}
	return unique(out)
}
func selectedValue(value json.RawMessage, path []string) (json.RawMessage, error) {
	for _, part := range path {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) == nil && object != nil {
			next, ok := object[part]
			if !ok {
				return nil, ErrInvalid
			}
			value = next
			continue
		}
		var array []json.RawMessage
		i, err := strconv.Atoi(part)
		if err != nil || strconv.Itoa(i) != part || json.Unmarshal(value, &array) != nil || i < 0 || i >= len(array) {
			return nil, ErrInvalid
		}
		value = array[i]
	}
	if !json.Valid(value) {
		return nil, ErrInvalid
	}
	return value, nil
}
func (s *Service) generationSource(ctx context.Context, p access.Principal, w string, g *GenerationPolicy) (any, []Ref, error) {
	if !generationFields(g) {
		return nil, nil, ErrInvalid
	}
	run, err := s.Runs.Read(ctx, p, w, g.Source.Run)
	if err != nil {
		return nil, nil, err
	}
	var head struct {
		Input Ref    `json:"input"`
		State string `json:"state"`
	}
	if run.Kind != "run" || json.Unmarshal(run.Value, &head) != nil {
		return nil, nil, ErrState
	}
	deps := []Ref{run.Ref}
	excerpt := func(ref Ref, path []string) (any, error) {
		r, err := s.Runs.Read(ctx, p, w, ref)
		if err != nil {
			return nil, err
		}
		if r.Kind != "output" && r.Kind != "pause" {
			return nil, ErrInvalid
		}
		root, err := s.Runs.Read(ctx, p, w, Ref{ID: r.RunID, Version: "head"})
		if err != nil {
			return nil, err
		}
		var parent struct {
			Input Ref `json:"input"`
		}
		json.Unmarshal(root.Value, &parent)
		input, err := s.Runs.Read(ctx, p, w, parent.Input)
		if err != nil {
			return nil, err
		}
		deps = append(deps, r.Ref)
		deps = append(deps, r.Refs...)
		deps = append(deps, input.Refs...)
		deps = append(deps, root.Ref)
		var body map[string]json.RawMessage
		json.Unmarshal(r.Value, &body)
		value := body["value"]
		if r.Kind == "pause" {
			value = body["response"]
		}
		selected, err := selectedValue(value, path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ref": r.Ref, "run_id": r.RunID, "path": path, "digest": hash(value), "selected_utf8": string(selected)}, nil
	}
	output, err := s.Runs.Read(ctx, p, w, g.Source.Output)
	if err != nil || output.RunID != run.ID || output.Kind != "output" {
		return nil, nil, ErrInvalid
	}
	rootPosition, _ := strconv.ParseInt(run.Position, 10, 64)
	outputPosition, _ := strconv.ParseInt(output.Position, 10, 64)
	if outputPosition > rootPosition {
		return nil, nil, ErrInvalid
	}
	primary, err := excerpt(g.Source.Output, g.Source.Path)
	if err != nil {
		return nil, nil, err
	}
	extras := []any{}
	for _, e := range g.Source.Extras {
		v, err := excerpt(e.Ref, e.Path)
		if err != nil {
			return nil, nil, err
		}
		extras = append(extras, v)
	}
	groups := []any{}
	for _, ref := range g.GroupScopes {
		r, err := s.Contexts.Read(ctx, p, w, ref)
		if err != nil {
			return nil, nil, err
		}
		var scope contexts.Scope
		json.Unmarshal(r.Value, &scope)
		groups = append(groups, map[string]any{"ref": ref, "kind": scope.Kind, "name": scope.Name})
		deps = append(deps, Ref{ID: "scope:" + ref.ID, Version: ref.Revision})
	}
	return map[string]any{"run": g.Source.Run, "output": primary, "extras": extras, "group_scopes": groups}, unique(deps), nil
}

// GenerationRead returns only the explicitly selected fixed excerpts. Run inputs,
// other channels and unrelated dialogue never enter the model response.
func (s *Service) GenerationRead(ctx context.Context, p access.Principal, w string, ref Ref) (any, []Ref, bool, error) {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found || op.Purpose != "context-generation" {
		return nil, nil, false, err
	}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), access.Principal{UserID: p.UserID}, w, op)
	if err != nil || policy.Generation == nil || ref != policy.Generation.Source.Output {
		return nil, nil, true, access.ErrForbidden
	}
	value, deps, err := s.generationSource(ctx, access.Principal{UserID: p.UserID}, w, policy.Generation)
	if err == nil {
		err = s.checkRefs(ctx, s.Store.Database(), access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}, w, deps)
	}
	// Dependencies are checked above and again by CheckMCPPolicy at delivery;
	// the audit records only the source/metadata references actually returned.
	return value, generationRefs(policy.Generation), true, err
}
