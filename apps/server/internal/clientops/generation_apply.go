package clientops

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/jackc/pgx/v5"
)

func (s *Service) adoptGeneration(ctx context.Context, tx pgx.Tx, p access.Principal, w string, operation Ref, op *Operation) error {
	if op.Proposal == nil {
		return ErrState
	}
	policy, err := s.currentPolicy(ctx, tx, p, w, *op)
	if err != nil || policy.Generation == nil {
		return ErrState
	}
	_, sourceDeps, err := s.generationSource(ctx, p, w, policy.Generation)
	if err != nil {
		return err
	}
	r, err := record(ctx, tx, w, *op.Proposal)
	if err != nil {
		return err
	}
	var proposal Proposal
	if decode(r.Value, &proposal) != nil || !complete(proposal) {
		return ErrState
	}
	if !proposal.Manual {
		if err = s.checkRefs(ctx, tx, access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}, w, append(sourceDeps, generationRefs(policy.Generation)...)); err != nil {
			return err
		}
		var fetched bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM access_operation_events WHERE workspace_id=$1 AND operation_id=$2 AND method='run_get' AND result->>'outcome'='returned' AND result->>'range'='fixed_output_excerpt' AND ($3::timestamptz IS NULL OR created_at>=$3) AND result->'refs' @> $4::jsonb)`, w, op.ReadOperation, op.EvidenceSince, raw([]Ref{policy.Generation.Source.Output})).Scan(&fetched); err != nil {
			return err
		}
		if !fetched {
			return ErrState
		}
	}
	var changes GenerationProposal
	if decode([]byte(proposal.ChangesUTF8), &changes) != nil || changes.Changes == nil || changes.Grouping == nil || changes.Candidates == nil {
		return ErrInvalid
	}
	selected := func(key string) bool {
		if len(op.AdoptPaths) == 0 {
			return true
		}
		for _, k := range op.AdoptPaths {
			if k == key {
				return true
			}
		}
		return false
	}
	// The common validator owns all four content levels and membership permissions.
	content := s.Contexts.InTransaction(tx)
	aliases := map[string]contexts.Ref{}
	keys := map[string]bool{}
	levels := map[string]int{"sentence": 1, "paragraph": 2, "section": 3, "document": 4}
	for _, c := range changes.Changes {
		if c.Key == "" || keys[c.Key] || levels[c.Target.Kind] == 0 || c.Reason == "" || (c.State != "observed" && c.State != "inference" && c.State != "judgment" && c.State != "proposal") {
			return ErrInvalid
		}
		keys[c.Key] = true
	}
	sort.SliceStable(changes.Changes, func(i, j int) bool {
		return levels[changes.Changes[i].Target.Kind] < levels[changes.Changes[j].Target.Kind]
	})
	known, err := s.mcpSources(ctx, tx, w, op.ReadOperation, op.EvidenceSince, true, true)
	if err != nil {
		return err
	}
	seen := map[Ref]bool{}
	for _, ref := range known {
		seen[ref] = true
	}
	for _, lock := range policy.Generation.Fixed {
		seen[Ref{ID: resource(lock.Ref), Version: lock.Ref.Revision}] = true
	}
	result := GenerationResult{Operation: operation, Source: policy.Generation.Source, Items: []GeneratedContext{}, Memberships: []contexts.Ref{}, Candidates: changes.Candidates}
	refs := append([]Ref{operation, *op.Proposal}, sourceDeps...)
	check := func(ref contexts.Ref) error {
		if !fixed(ref) || (!proposal.Manual && !seen[Ref{ID: resource(ref), Version: ref.Revision}]) {
			return ErrState
		}
		reader := access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}
		if proposal.Manual {
			reader = p
		}
		_, err := content.ReadTx(ctx, tx, reader, w, ref)
		return err
	}
	for _, candidate := range changes.Candidates {
		if candidate.Reason == "" || (candidate.Relation != "reuse" && candidate.Relation != "duplicate" && candidate.Relation != "contradiction") {
			return ErrInvalid
		}
		if err = check(candidate.Ref); err != nil {
			return err
		}
		refs = append(refs, Ref{ID: resource(candidate.Ref), Version: candidate.Ref.Revision})
	}
	var source struct {
		Value json.RawMessage `json:"value"`
	}
	output, err := s.Runs.Read(ctx, p, w, policy.Generation.Source.Output)
	if err != nil {
		return err
	}
	json.Unmarshal(output.Value, &source)
	pointer := ""
	for _, part := range policy.Generation.Source.Path {
		pointer += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	for _, change := range changes.Changes {
		if !selected(change.Key) {
			continue
		}
		target := change.Target
		base := map[string]any{"kind": target.Kind}
		var previous contexts.Unit
		if target.Revision == "0" {
			if target.ID != "" && target.ID != "$"+change.Key {
				return ErrInvalid
			}
			target.ID = "generated-" + hash([]string{operation.ID, change.Key})
		} else {
			if err = check(target); err != nil {
				return err
			}
			rec, err := content.ReadTx(ctx, tx, p, w, target)
			if err != nil {
				return err
			}
			base, err = definitionObject(rec.Value)
			if err != nil {
				return err
			}
			json.Unmarshal(rec.Value, &previous)
			refs = append(refs, Ref{ID: resource(target), Version: target.Revision})
		}

		for _, lock := range policy.Generation.Fixed {
			if proposal.Manual || lock.Ref.Identity() != target.Identity() {
				continue
			}
			if lock.Ref.Revision != target.Revision {
				return ErrState
			}
			for name, value := range lock.Fields {
				if value == "" {
					delete(base, name)
				} else {
					var fixedValue any
					decoder := json.NewDecoder(strings.NewReader(value))
					decoder.UseNumber()
					if decoder.Decode(&fixedValue) != nil {
						return ErrInvalid
					}
					base[name] = fixedValue
				}
			}
		}
		proposed, err := definitionObject([]byte(change.ValueUTF8))
		if err != nil {
			return err
		}
		proposed = mergeDefinition(base, proposed)
		for _, lock := range policy.Generation.Fixed {
			if lock.Ref.Identity() != target.Identity() {
				continue
			}
			if lock.Ref.Revision != target.Revision {
				return ErrState
			}
			for fieldName, fixedValue := range lock.Fields {
				value, present := proposed[fieldName]
				if fixedValue == "" {
					if present {
						return ErrState
					}
				} else {
					var expected any
					decoder := json.NewDecoder(strings.NewReader(fixedValue))
					decoder.UseNumber()
					if decoder.Decode(&expected) != nil || !bytes.Equal(raw(value), raw(expected)) {
						return ErrState
					}
				}
			}
		}
		var unit contexts.Unit
		if decode(raw(proposed), &unit) != nil || unit.Kind != target.Kind {
			return ErrInvalid
		}
		if target.Revision == "0" {
			if len(unit.Tags) > 0 && !bytes.Equal(raw(unit.Tags), raw(policy.Generation.Tags)) {
				return access.ErrForbidden
			}
			unit.Tags = policy.Generation.Tags
		} else if !proposal.Manual && !bytes.Equal(raw(unit.Tags), raw(previous.Tags)) {
			return access.ErrForbidden
		}
		for i, child := range unit.Children {
			if strings.HasPrefix(child.ID, "$") {
				ref, ok := aliases[strings.TrimPrefix(child.ID, "$")]
				if !ok || ref.Kind != child.Kind {
					return ErrState
				}
				unit.Children[i] = ref
			} else if err = check(child); err != nil {
				return err
			}
		}
		for _, ref := range change.References {
			if err = check(ref); err != nil {
				return err
			}
			refs = append(refs, Ref{ID: resource(ref), Version: ref.Revision})
		}
		// Origin is derived from the fixed source, never trusted from model text.
		unit.Origin = &contexts.Origin{Kind: "run-output", ID: policy.Generation.Source.Output.ID, Revision: policy.Generation.Source.Output.Version, Digest: hash(source.Value), Path: pointer, OperationID: operation.ID}
		saved, err := accepted(content.Put(ctx, p, w, target.Identity(), contexts.Write{Mutation: apicontract.Mutation{OperationID: "generated-context:" + op.Proposal.ID + ":" + change.Key, ExpectedVersion: target.Revision}, Value: raw(unit)}))
		if err != nil {
			return err
		}
		ref := contexts.Ref{Kind: target.Kind, ID: target.ID, Revision: saved.Version}
		aliases[change.Key] = ref
		item := GeneratedContext{Name: contexts.CandidateOf(contexts.Record{Value: raw(unit)}).Name, Key: change.Key, Ref: ref, State: change.State, Reason: change.Reason, References: change.References}
		origin := GenerationOrigin{Manual: proposal.Manual, Operation: operation, Policy: op.Policy, Source: policy.Generation.Source, Item: item}
		dependencies := append(append([]Ref{}, sourceDeps...), Ref{ID: resource(ref), Version: ref.Revision})
		for _, r := range change.References {
			dependencies = append(dependencies, Ref{ID: resource(r), Version: r.Revision})
		}
		if _, err = tx.Exec(ctx, `INSERT INTO client_context_origins VALUES($1,$2,$3,$4,$5,$6)`, w, ref.ID, ref.Revision, operation.ID, raw(origin), raw(unique(dependencies))); err != nil {
			return err
		}
		result.Items = append(result.Items, item)
		refs = append(refs, saved)
	}
	for _, group := range changes.Grouping {
		if !selected(group.Key) {
			continue
		}
		if group.Key == "" || keys[group.Key] || group.ExpectedVersion == "" {
			return ErrInvalid
		}
		keys[group.Key] = true
		allowed := false
		for _, ref := range policy.Generation.GroupScopes {
			allowed = allowed || ref.ID == group.Scope.ID
		}
		if !allowed {
			return access.ErrForbidden
		}
		target := group.Context
		if strings.HasPrefix(target.ID, "$") {
			ref, ok := aliases[strings.TrimPrefix(target.ID, "$")]
			if !ok || ref.Kind != target.Kind {
				return ErrState
			}
			target = ref.Identity()
		} else {
			allowed = false
			for _, ref := range aliases {
				allowed = allowed || ref.Identity() == target
			}
			if !allowed {
				return ErrState
			}
		}
		if err = s.Access.CheckTx(ctx, tx, p, w, "scope:"+group.Scope.ID, "update"); err != nil {
			return err
		}
		id := group.ID
		if id == "" {
			var existingID, version string
			var present bool
			err = tx.QueryRow(ctx, `SELECT c.resource_id,c.revision,(c.payload->>'present')::boolean FROM context_records c JOIN shared_versions v ON v.workspace_id=c.workspace_id AND v.resource_id=c.resource_id AND v.version=c.revision WHERE c.workspace_id=$1 AND c.kind='membership' AND c.payload->'context'->>'id'=$2 AND c.payload->'scope'->>'id'=$3 ORDER BY c.resource_id LIMIT 1`, w, target.ID, group.Scope.ID).Scan(&existingID, &version, &present)
			if err == nil {
				if present != group.Present {
					return ErrState
				}
				reused := contexts.Ref{Kind: "membership", ID: strings.TrimPrefix(existingID, "membership:"), Revision: version}
				if _, err = content.ReadTx(ctx, tx, p, w, reused); err != nil {
					return err
				}
				result.Memberships = append(result.Memberships, reused)
				refs = append(refs, Ref{ID: existingID, Version: version})
				continue
			}
			if err != pgx.ErrNoRows {
				return err
			}
			id = "generated-group-" + hash([]any{target, group.Scope})
		}
		saved, err := accepted(content.Put(ctx, p, w, contexts.Identity{Kind: "membership", ID: id}, contexts.Write{Mutation: apicontract.Mutation{OperationID: "generated-group:" + op.Proposal.ID + ":" + group.Key, ExpectedVersion: group.ExpectedVersion}, Value: raw(contexts.Membership{Scope: group.Scope, Context: target, Present: group.Present})}))
		if err != nil {
			return err
		}
		ref := contexts.Ref{Kind: "membership", ID: id, Revision: saved.Version}
		result.Memberships = append(result.Memberships, ref)
		refs = append(refs, saved)
	}
	saved, err := accepted(s.mutate(ctx, p, w, op.Target.ID, apicontract.Mutation{OperationID: "generated-result:" + op.Proposal.ID, ExpectedVersion: op.Target.Version}, result, func(tx pgx.Tx, ref Ref) (Record, error) {
		return s.save(ctx, tx, w, p, ref, "context-result", result, unique(refs), operation.ID)
	}, tx))
	if err != nil {
		return err
	}
	op.Applied = &saved
	op.State = "applied"
	op.Reason = ""
	return nil
}
