package clientops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

type GenerationRule struct {
	Name     string   `json:"name"`
	Goal     string   `json:"goal"`
	Channel  string   `json:"channel"`
	Targets  []string `json:"targets"`
	Template Policy   `json:"template"`
}
type GenerationActivation struct {
	Rule    Ref    `json:"rule"`
	Enabled bool   `json:"enabled"`
	User    string `json:"user"`
	Client  string `json:"client"`
	Model   Model  `json:"model"`
}
type EnableGeneration struct {
	apicontract.Mutation
	Rule    Ref    `json:"rule"`
	Enabled bool   `json:"enabled"`
	Client  string `json:"client"`
	Model   Model  `json:"model"`
}

func validGenerationRule(v GenerationRule) bool {
	if v.Name == "" || v.Goal == "" || v.Channel == "" || len(v.Targets) == 0 || v.Template.Generation == nil || v.Template.Definition != nil || (v.Template.Mode != "auto" && v.Template.Mode != "proposal" && v.Template.Mode != "manual") || (v.Template.Fixed.Mode != "select" && v.Template.Fixed.Mode != "none") || v.Template.Limits.Models < 1 || v.Template.Limits.MCP < 1 || v.Template.Limits.Seconds < 1 {
		return false
	}
	for _, target := range v.Targets {
		if target != "*" && !strings.HasPrefix(target, "run:") && !strings.HasPrefix(target, "scope:") && !strings.HasPrefix(target, "run-definition:") {
			return false
		}
	}
	g := *v.Template.Generation
	if g.Source.Run.ID != "" || g.Source.Output.ID != "" || g.Activation != nil {
		return false
	}
	// Validate the options using syntactic fixed references, without persisting or
	// authorizing a source before an output has actually been finalized.
	g.Source.Run = Ref{ID: "run:validation", Version: "validation"}
	g.Source.Output = Ref{ID: "run-output:validation", Version: "validation"}
	return generationFields(&g)
}
func (s *Service) PutGenerationRule(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	var rule GenerationRule
	if !strings.HasPrefix(id, "generation-rule:") || decode(req.Value, &rule) != nil || !validGenerationRule(rule) || p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		if !s.Contexts.InTransaction(tx).Preview(ctx, p, w, contexts.ResolveRequest{Selection: rule.Template.Fixed}).Ready {
			return Record{}, ErrInvalid
		}
		deps := refs(rule.Template.Fixed)
		for _, id := range rule.Template.Scopes {
			if err := s.Access.CheckTx(ctx, tx, p, w, "scope:"+id, "get"); err != nil {
				return Record{}, err
			}
			deps = append(deps, Ref{ID: "scope:" + id})
		}
		for _, lock := range rule.Template.Generation.Fixed {
			deps = append(deps, Ref{ID: resource(lock.Ref), Version: lock.Ref.Revision})
		}
		for _, ref := range rule.Template.Generation.GroupScopes {
			deps = append(deps, Ref{ID: "scope:" + ref.ID, Version: ref.Revision})
		}
		for _, extra := range rule.Template.Generation.Source.Extras {
			if _, err := s.Runs.Read(ctx, p, w, extra.Ref); err != nil {
				return Record{}, err
			}
			deps = append(deps, extra.Ref)
		}
		for _, target := range rule.Targets {
			if target != "*" {
				if err := s.Access.CheckTx(ctx, tx, p, w, target, "get"); err != nil {
					return Record{}, err
				}
			}
		}
		if err := s.checkRefs(ctx, tx, p, w, deps); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, p, ref, "generation-rule", rule, unique(deps))
	})
}
func (s *Service) EnableGeneration(ctx context.Context, p access.Principal, w string, req EnableGeneration) (apicontract.Response[apicontract.Reference], error) {
	if !pinned(req.Rule) || !strings.HasPrefix(req.Rule.ID, "generation-rule:") || p.OperationID != "" || (req.Enabled && (req.Client == "" || !validModel(req.Model))) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	id := "generation-active:" + strings.TrimPrefix(req.Rule.ID, "generation-rule:")
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		old, err := record(ctx, tx, w, Ref{ID: id, Version: "head"})
		var previous GenerationActivation
		if err == nil {
			json.Unmarshal(old.Value, &previous)
			if previous.User != p.UserID {
				if req.Enabled || s.Access.CheckTx(ctx, tx, p, w, "", "manage") != nil {
					return Record{}, access.ErrForbidden
				}
			}
		} else if err != pgx.ErrNoRows {
			return Record{}, err
		}
		r, err := record(ctx, tx, w, req.Rule)
		if err != nil || r.Kind != "generation-rule" {
			return Record{}, ErrInvalid
		}
		if err = s.checkRefs(ctx, tx, p, w, append(r.Refs, r.Ref)); err != nil {
			return Record{}, err
		}
		var rule GenerationRule
		if decode(r.Value, &rule) != nil || !validGenerationRule(rule) || (req.Enabled && rule.Template.Mode == "manual") {
			return Record{}, ErrInvalid
		}
		activation := GenerationActivation{Rule: req.Rule, Enabled: req.Enabled, User: p.UserID, Client: req.Client, Model: req.Model}
		if !req.Enabled && previous.User != "" {
			activation = previous
			activation.Enabled = false
		}
		if _, err = tx.Exec(ctx, `DELETE FROM context_generation_targets WHERE workspace_id=$1 AND activation_id=$2`, w, id); err != nil {
			return Record{}, err
		}
		if req.Enabled {
			for _, target := range rule.Targets {
				if _, err = tx.Exec(ctx, `INSERT INTO context_generation_targets VALUES($1,$2,$3)`, w, target, id); err != nil {
					return Record{}, err
				}
			}
		}
		return s.save(ctx, tx, w, p, ref, "generation-activation", activation, append(r.Refs, r.Ref))
	})
}
func (s *Service) generationControl(ctx context.Context, q query, p access.Principal, w string, ref *Ref) error {
	if ref == nil {
		return nil
	}
	r, err := record(ctx, q, w, *ref)
	if err != nil {
		return err
	}
	var activation GenerationActivation
	if r.Kind != "generation-activation" || decode(r.Value, &activation) != nil || !activation.Enabled || activation.User != p.UserID {
		return ErrState
	}
	var head string
	if err = q.QueryRow(ctx, `SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2`, w, ref.ID).Scan(&head); err != nil {
		return err
	}
	if head != ref.Version {
		return ErrState
	}
	return s.checkRefs(ctx, q, p, w, append(r.Refs, r.Ref))
}
func (s *Service) observeGenerationSource(ctx context.Context, tx pgx.Tx, command persistence.Command, ref apicontract.Reference, state json.RawMessage) error {
	if command.Kind != "run-event" || command.Deleted {
		return nil
	}
	var event struct {
		Type string `json:"type"`
	}
	json.Unmarshal(command.Payload, &event)
	if event.Type != "finished" && event.Type != "cancelled" {
		return nil
	}
	var envelope struct {
		Value runs.Run `json:"value"`
		Refs  []Ref    `json:"refs"`
	}
	if json.Unmarshal(state, &envelope) != nil || (envelope.Value.State != "succeeded" && envelope.Value.State != "failed" && envelope.Value.State != "cancelled") {
		return nil
	}
	var inputRefs []byte
	if err := tx.QueryRow(ctx, `SELECT refs FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='input'`, command.WorkspaceID, envelope.Value.Input.ID, envelope.Value.Input.Version).Scan(&inputRefs); err != nil {
		return err
	}
	var dependencies []Ref
	json.Unmarshal(inputRefs, &dependencies)
	dependencies = unique(append(append(dependencies, envelope.Refs...), Ref{ID: ref.ID, Version: ref.Version}))
	keys := []string{"*", ref.ID}
	for _, r := range dependencies {
		if strings.HasPrefix(r.ID, "scope:") || strings.HasPrefix(r.ID, "run-definition:") {
			keys = append(keys, r.ID)
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT activation_id FROM context_generation_targets WHERE workspace_id=$1 AND target=ANY($2)`, command.WorkspaceID, keys)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	var through int64
	if err = tx.QueryRow(ctx, `SELECT sequence FROM shared_streams WHERE workspace_id=$1`, command.WorkspaceID).Scan(&through); err != nil {
		return err
	}
	for _, id := range ids {
		r, err := record(ctx, tx, command.WorkspaceID, Ref{ID: id, Version: "head"})
		if err != nil {
			return err
		}
		var activation GenerationActivation
		if decode(r.Value, &activation) != nil || !activation.Enabled {
			continue
		}
		owner := access.Principal{UserID: activation.User}
		if s.Access.CheckTx(ctx, tx, owner, command.WorkspaceID, "", "register") != nil || s.checkRefs(ctx, tx, owner, command.WorkspaceID, dependencies) != nil {
			continue
		}
		qid := hash([]Ref{activation.Rule, {ID: ref.ID, Version: ref.Version}})
		if _, err = tx.Exec(ctx, `INSERT INTO context_generation_queue(workspace_id,id,activation_id,activation_version,run_id,run_version,through,owner_user,client_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, command.WorkspaceID, qid, r.ID, r.Version, ref.ID, ref.Version, through, activation.User, activation.Client); err != nil {
			return err
		}
	}
	return nil
}
