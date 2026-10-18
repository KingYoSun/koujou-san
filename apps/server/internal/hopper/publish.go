package hopper

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

func (s *Service) lineage(ctx context.Context, tx pgx.Tx, w, id string, e *Evaluation, ref Ref) error {
	// Creation may already have inherited its parent's lineage. This explicit
	// Hopper action is the new edge and advances depth exactly once.
	_, err := tx.Exec(ctx, `INSERT INTO hopper_lineage VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,resource_id) DO UPDATE SET root=excluded.root,depth=excluded.depth,evaluation_id=excluded.evaluation_id,evaluation_version=excluded.evaluation_version`, w, id, e.Root, e.Depth+1, ref.ID, ref.Version)
	return err
}
func graphRef(r Ref) contexts.Ref {
	k, id, _ := strings.Cut(r.ID, ":")
	return contexts.Ref{Kind: k, ID: id, Revision: r.Version}
}
func (s *Service) relation(ctx context.Context, tx pgx.Tx, p access.Principal, w string, e *Evaluation, ref, dest Ref) error {
	if s.Graph == nil {
		return ErrState
	}
	_, err := accepted(s.Graph.PutRelation(ctx, p, w, "hopper-"+hash([]Ref{ref, dest}), graphindex.RelationWrite{OperationID: "hopper-relation:" + ref.ID + ":" + dest.ID, ExpectedVersion: "0", Value: graphindex.Relation{Kind: "reference", From: graphRef(e.Source.Target), To: graphRef(dest), Present: true, Origin: &contexts.Origin{Kind: "hopper-evaluation", ID: ref.ID, Revision: ref.Version, Digest: hash(e.Source)}}}, tx))
	return err
}
func (s *Service) publish(ctx context.Context, tx pgx.Tx, p access.Principal, w string, d Definition, e *Evaluation, ref, control Ref) (Ref, error) {
	suffix := "hopper-" + hash([]string{ref.ID})
	provenance := append(append([]Ref{}, e.Source.Refs...), e.Source.Target, d.Action.Template, e.Definition)
	provenance = append(provenance, e.protected...)
	templateRefs, err := s.template(ctx, p, w, d)
	if err != nil {
		return Ref{}, err
	}
	provenance = append(provenance, templateRefs...)
	if err = s.refs(ctx, tx, p, w, provenance); err != nil {
		return Ref{}, err
	}
	fixedSources := []Ref{}
	for _, source := range provenance {
		if fixed(source) {
			fixedSources = append(fixedSources, source)
		}
	}
	if d.Action.Kind == "run" {
		r, err := s.Workflows.Read(ctx, p, w, d.Action.Template)
		if err != nil {
			return Ref{}, err
		}
		var def workflows.Definition
		_ = json.Unmarshal(r.Value, &def)
		spec := *def.Spec
		spec.Sources = append(append([]Ref{}, spec.Sources...), fixedSources...)
		id := "run:" + suffix
		repository := d.Action.Repository
		if repository != nil {
			copy := *repository
			repository = &copy
		}
		for _, m := range d.Action.Mappings {
			value, ok := selected(evaluationData(e), m.Path)
			if !ok {
				return Ref{}, ErrState
			}
			switch m.To {
			case "repository.commit":
				commit, ok := value.(string)
				if !ok || repository == nil {
					return Ref{}, ErrInvalid
				}
				repository.Commit = commit
			case "prompt":
				spec.Input.Prompt = raw(value)
			case "command":
				spec.Input.Command.Parameters = raw(value)
			case "context":
				if def.Selection != nil && def.Selection.Mode == "none" {
					return Ref{}, ErrInvalid
				}
				spec.Input.Context = raw(value)
			}
		}
		if !runs.ValidRepository(repository) {
			return Ref{}, ErrInvalid
		}
		if def.Selection != nil {
			c := s.Runs.Contexts.InTransaction(tx)
			resolved := c.Preview(ctx, p, w, contexts.ResolveRequest{RunID: id, Selection: *def.Selection})
			if !resolved.Ready || resolved.Manifest == nil {
				return Ref{}, ErrState
			}
			manifest, err := accepted(c.Put(ctx, p, w, contexts.Identity{Kind: "manifest", ID: suffix}, contexts.Write{Mutation: mutation("manifest:" + ref.ID), Value: raw(contexts.ResolveRequest{RunID: id, Selection: *def.Selection})}))
			if err != nil {
				return Ref{}, err
			}
			spec.Manifest = &contexts.Ref{Kind: "manifest", ID: strings.TrimPrefix(manifest.ID, "manifest:"), Revision: manifest.Version}
		}
		runner := s.Runs.InTransaction(tx)
		destination, err := accepted(runner.Create(ctx, p, w, id, runs.Create{Mutation: mutation("run:" + ref.ID), Spec: spec}))
		if err != nil {
			return Ref{}, err
		}
		if err = s.lineage(ctx, tx, w, destination.ID, e, ref); err != nil {
			return Ref{}, err
		}
		if d.Action.Start {
			destination, err = accepted(runner.Action(ctx, p, w, id, "request-start", structMutation("start:"+ref.ID, destination.Version)))
			if err != nil {
				return Ref{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO hopper_dispatch(workspace_id,run_id,evaluation_id,evaluation_version,definition,controller,preferred_device,repository,required) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, w, id, ref.ID, ref.Version, raw(e.Definition), p.UserID, def.DeviceID, raw(repository), raw(def.Required)); err != nil {
				return Ref{}, err
			}
		}
		return destination, s.relation(ctx, tx, p, w, e, ref, destination)
	}
	r, err := s.Publications.Read(ctx, p, w, d.Action.Template)
	if err != nil {
		return Ref{}, err
	}
	var draft publications.Draft
	_ = json.Unmarshal(r.Value, &draft)
	for _, m := range d.Action.Mappings {
		value, ok := selected(evaluationData(e), m.Path)
		if !ok {
			return Ref{}, ErrState
		}
		text, ok := value.(string)
		if !ok {
			return Ref{}, ErrInvalid
		}
		if m.To == "title" {
			draft.Title = text
		} else {
			draft.Body = text
		}
	}
	issue, err := accepted(s.Runs.Contexts.InTransaction(tx).Put(ctx, p, w, contexts.Identity{Kind: "scope", ID: suffix}, contexts.Write{Mutation: mutation("issue:" + ref.ID), Value: raw(contexts.Scope{Kind: "issue", Name: draft.Title, Parent: d.Action.Parent})}))
	if err != nil {
		return Ref{}, err
	}
	if _, err = accepted(s.Automation.Put(ctx, p, w, "issue:"+suffix, automation.Write{Mutation: mutation("hopper-issue-info:" + suffix), Value: raw(automation.Issue{Scope: issue, Origin: "hopper", Title: draft.Title, Body: draft.Body, State: "open", Dependencies: []Ref{}, Sources: fixedSources})}, tx)); err != nil {
		return Ref{}, err
	}
	if err = s.lineage(ctx, tx, w, issue.ID, e, ref); err != nil {
		return Ref{}, err
	}
	draft.Sources = append(append(draft.Sources, fixedSources...), issue)
	publisher := s.Publications.InTransaction(tx)
	pin, err := accepted(publisher.Put(ctx, p, w, "publication-draft:"+suffix, publications.Write{Mutation: mutation("draft:" + ref.ID), Value: raw(draft)}))
	if err != nil {
		return Ref{}, err
	}
	destination, err := accepted(publisher.SubmitAutomation(ctx, w, "publication:"+suffix, publications.Submit{Mutation: mutation("publish:" + ref.ID), Draft: pin}, control))
	if err != nil {
		return Ref{}, err
	}
	if err = s.lineage(ctx, tx, w, destination.ID, e, ref); err != nil {
		return Ref{}, err
	}
	if err = s.lineage(ctx, tx, w, "run:"+destination.ID, e, ref); err != nil {
		return Ref{}, err
	}
	if err = s.relation(ctx, tx, p, w, e, ref, issue); err != nil {
		return Ref{}, err
	}
	return destination, s.relation(ctx, tx, p, w, e, ref, destination)
}
