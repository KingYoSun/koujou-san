package clientops

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type DefinitionPolicy struct {
	FixedValues map[string]string `json:"fixed_values,omitempty"`
	FixedPaths  []string          `json:"fixed_paths"`
	Sources     []Ref             `json:"sources"`
}

func (s *Service) DefinitionSources(ctx context.Context, p access.Principal, w string) ([]Ref, bool, error) {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found || op.Purpose != "definition" {
		return nil, false, err
	}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), access.Principal{UserID: p.UserID}, w, op)
	if err != nil || policy.Definition == nil {
		return nil, true, ErrState
	}
	refs := append([]Ref{}, policy.Definition.Sources...)
	if op.Target.Version != "0" {
		refs = append(refs, op.Target)
	}
	return unique(refs), true, nil
}

// These are the same editable fields of run-definition-v1, grouped for adoption.
var DefinitionPaths = []string{"/name", "/device_id", "/spec/name", "/spec/issue", "/spec/input/command/kind", "/spec/input/command/target", "/spec/input/command/parameters", "/spec/input/command/binding", "/spec/input/prompt", "/spec/input/context", "/spec/derivation", "/spec/sources", "/spec/previous", "/spec/artifacts", "/selection", "/inputs", "/required", "/ci", "/write", "/baseline"}

func definitionPath(path string) bool {
	for _, allowed := range DefinitionPaths {
		if path == allowed {
			return true
		}
	}
	return false
}
func validPolicyKind(id string, p Policy) bool {
	if p.Generation != nil {
		return p.Definition == nil && strings.HasPrefix(id, "context-policy:") && generationFields(p.Generation)
	}
	if p.Definition == nil {
		return strings.HasPrefix(id, "selection-policy:")
	}
	if !strings.HasPrefix(id, "definition-policy:") {
		return false
	}
	for _, path := range p.Definition.FixedPaths {
		if !definitionPath(path) {
			return false
		}
	}
	for path, value := range p.Definition.FixedValues {
		found := false
		for _, fixed := range p.Definition.FixedPaths {
			found = found || fixed == path
		}
		if !found || (value != "" && !json.Valid([]byte(value))) {
			return false
		}
	}
	for _, ref := range p.Definition.Sources {
		if !pinned(ref) || (!strings.HasPrefix(ref.ID, "run-definition:") && !strings.HasPrefix(ref.ID, "runner:")) {
			return false
		}
	}
	return true
}
func (s *Service) target(ctx context.Context, tx pgx.Tx, p access.Principal, w string, ref Ref, purpose string) (Record, error) {
	if purpose != "definition" {
		return record(ctx, tx, w, ref)
	}
	if s.Definitions == nil {
		return Record{}, ErrState
	}
	r, err := s.Definitions.InTransaction(tx).Read(ctx, p, w, ref)
	return Record{Ref: r.Ref, Kind: r.Kind, Value: r.Value, Refs: r.Refs}, err
}
func decodeOperationProposal(b []byte, purpose string, p *Proposal) error {
	if purpose == "context-generation" {
		var v struct {
			ChangesUTF8 string   `json:"changes_utf8"`
			Reason      string   `json:"reason"`
			Questions   []string `json:"questions"`
			Unknown     []string `json:"unknown"`
		}
		if decode(b, &v) != nil || v.Questions == nil || v.Unknown == nil || v.Reason == "" || !json.Valid([]byte(v.ChangesUTF8)) {
			return ErrInvalid
		}
		var changes GenerationProposal
		if decode([]byte(v.ChangesUTF8), &changes) != nil || changes.Changes == nil || changes.Grouping == nil || changes.Candidates == nil {
			return ErrInvalid
		}
		*p = Proposal{Items: []Item{}, ChangesUTF8: v.ChangesUTF8, Reason: v.Reason, Questions: v.Questions, Unknown: v.Unknown}
		return nil
	}

	if purpose != "definition" {
		return decodeProposal(b, p)
	}
	var v struct {
		DefinitionUTF8 string   `json:"definition_utf8"`
		Reason         string   `json:"reason"`
		Questions      []string `json:"questions"`
		Unknown        []string `json:"unknown"`
	}
	if decode(b, &v) != nil || v.Questions == nil || v.Unknown == nil || v.Reason == "" || !json.Valid([]byte(v.DefinitionUTF8)) {
		return ErrInvalid
	}
	*p = Proposal{Items: []Item{}, DefinitionUTF8: v.DefinitionUTF8, Reason: v.Reason, Questions: v.Questions, Unknown: v.Unknown}
	return nil
}

func definitionObject(b []byte) (map[string]any, error) {
	var object map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&object) != nil || object == nil {
		return nil, ErrInvalid
	}
	return object, nil
}
func field(object map[string]any, path string) (any, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, key := range parts {
		value, exists := object[key]
		if i == len(parts)-1 {
			return value, exists
		}
		object, _ = value.(map[string]any)
	}
	return nil, false
}
func copyField(to, from map[string]any, path string) {
	value, exists := field(from, path)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, key := range parts[:len(parts)-1] {
		next, ok := to[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			to[key] = next
		}
		to = next
	}
	if exists {
		to[parts[len(parts)-1]] = value
	} else {
		delete(to, parts[len(parts)-1])
	}
}
func mergeDefinition(base, changes map[string]any) map[string]any {
	out, _ := definitionObject(raw(base))
	for key, value := range changes {
		left, lok := out[key].(map[string]any)
		right, rok := value.(map[string]any)
		if lok && rok {
			value = mergeDefinition(left, right)
		}
		out[key] = value
	}
	return out
}

func (s *Service) adoptDefinition(ctx context.Context, tx pgx.Tx, p access.Principal, w string, operation Ref, op *Operation) error {
	if s.Definitions == nil || op.Proposal == nil {
		return ErrState
	}
	policy, err := s.currentPolicy(ctx, tx, p, w, *op)
	if err != nil || policy.Definition == nil {
		return ErrState
	}
	r, err := record(ctx, tx, w, *op.Proposal)
	if err != nil {
		return err
	}
	var proposal Proposal
	if decode(r.Value, &proposal) != nil || !complete(proposal) {
		return ErrState
	}
	proposed, err := definitionObject([]byte(proposal.DefinitionUTF8))
	if err != nil {
		return err
	}
	base := map[string]any{"schema": "run-definition-v1"}
	if op.Target.Version != "0" {
		old, err := s.target(ctx, tx, p, w, op.Target, "definition")
		if err != nil {
			return err
		}
		base, err = definitionObject(old.Value)
		if err != nil {
			return err
		}
	}
	for path, value := range policy.Definition.FixedValues {
		snapshot := map[string]any{}
		if value != "" {
			var v any
			decoder := json.NewDecoder(strings.NewReader(value))
			decoder.UseNumber()
			if decoder.Decode(&v) != nil {
				return ErrInvalid
			}
			parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
			at := snapshot
			for _, key := range parts[:len(parts)-1] {
				child := map[string]any{}
				at[key] = child
				at = child
			}
			at[parts[len(parts)-1]] = v
		}
		copyField(base, snapshot, path)
		if _, present := field(proposed, path); !present {
			copyField(proposed, base, path)
		}
	}
	if len(op.AdoptPaths) > 0 {
		merged, _ := definitionObject(raw(base))
		for _, path := range op.AdoptPaths {
			if !definitionPath(path) {
				return ErrInvalid
			}
			if _, present := field(proposed, path); present {
				copyField(merged, proposed, path)
			}
		}
		proposed = merged
	} else {
		proposed = mergeDefinition(base, proposed)
	}
	// Definition generation selects references; literal Context is authored manually.
	beforeContext, beforePresent := field(base, "/spec/input/context")
	afterContext, afterPresent := field(proposed, "/spec/input/context")
	if beforePresent != afterPresent || !bytes.Equal(raw(beforeContext), raw(afterContext)) {
		return ErrState
	}
	for _, path := range policy.Definition.FixedPaths {
		before, exists := field(base, path)
		after, present := field(proposed, path)
		if exists != present || !bytes.Equal(raw(before), raw(after)) {
			return ErrState
		}
	}
	var d workflows.Definition
	if decode(raw(proposed), &d) != nil || d.Schema != "run-definition-v1" || d.Spec == nil || d.DeviceID == "" {
		return ErrInvalid
	}
	beforeSelection := raw(d.Selection)
	if policy.Fixed.Mode == "none" {
		if d.Selection != nil && d.Selection.Mode != "none" {
			return ErrState
		}
	} else if d.Selection != nil || len(policy.Fixed.Include) > 0 || len(policy.Fixed.Exclude) > 0 {
		if d.Selection == nil {
			d.Selection = &contexts.Selection{}
		}
		selectionProposal := Proposal{Items: []Item{}, Questions: []string{}, Unknown: []string{}}
		for _, ref := range d.Selection.Include {
			selectionProposal.Items = append(selectionProposal.Items, Item{Ref: ref, Reason: proposal.Reason})
		}
		if err := s.candidateEvidence(ctx, tx, w, *op, policy, selectionProposal); err != nil {
			return err
		}
		input, err := selection(policy.Fixed, selectionProposal)
		if err != nil {
			return err
		}
		resolved, err := s.resolveSelection(ctx, tx, p, w, *op, input)
		if err != nil {
			return err
		}
		if !resolved.Ready {
			return ErrState
		}
		d.Selection = &input
	}
	for _, path := range policy.Definition.FixedPaths {
		if path == "/selection" && !bytes.Equal(beforeSelection, raw(d.Selection)) {
			return ErrState
		}
	}
	definitions := s.Definitions.InTransaction(tx)
	current, err := definitions.Capabilities(ctx, p, w, d.DeviceID, "head")
	if err != nil {
		return err
	}
	known := false
	for _, source := range policy.Definition.Sources {
		if source.ID != "runner:"+d.DeviceID {
			continue
		}
		declared, err := definitions.Capabilities(ctx, p, w, d.DeviceID, source.Version)
		if err != nil {
			return err
		}
		for _, before := range declared.Connections {
			for _, now := range current.Connections {
				if now.Kind == d.Spec.Input.Command.Kind && now.Target == d.Spec.Input.Command.Target && bytes.Equal(raw(before), raw(now)) {
					known = true
				}
			}
		}
	}
	if !known {
		return ErrState
	}
	definition, err := accepted(definitions.Put(ctx, p, w, op.Target.ID, workflows.Write{Mutation: apicontract.Mutation{OperationID: "definition:" + op.Proposal.ID, ExpectedVersion: op.Target.Version}, Value: raw(d)}))
	if err != nil {
		return err
	}
	checked, err := definitions.Validate(ctx, p, w, definition)
	if err != nil {
		return err
	}
	if !checked.Valid {
		return ErrState
	}
	head := func(id string) (string, error) {
		var version string
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')`, w, id).Scan(&version)
		return version, err
	}
	version, err := head("validation:" + definition.ID)
	if err != nil {
		return err
	}
	validation, err := accepted(definitions.Verify(ctx, p, w, workflows.Verify{Mutation: apicontract.Mutation{OperationID: "definition-check:" + op.Proposal.ID, ExpectedVersion: version}, Definition: definition}))
	if err != nil {
		return err
	}
	version, err = head("adoption:" + definition.ID)
	if err != nil {
		return err
	}
	adoption, err := accepted(definitions.Adopt(ctx, p, w, workflows.Adopt{Mutation: apicontract.Mutation{OperationID: "definition-adopt:" + op.Proposal.ID, ExpectedVersion: version}, Definition: definition, Validation: validation}))
	if err != nil {
		return err
	}
	op.Applied = &adoption
	op.State, op.Reason = "applied", ""
	return nil
}
