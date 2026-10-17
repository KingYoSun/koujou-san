package hopper

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

func (s *Service) active(ctx context.Context, tx pgx.Tx, w string, definition Ref) (Activation, error) {
	r, err := record(ctx, tx, w, Ref{ID: "hopper-active:" + strings.TrimPrefix(definition.ID, "hopper:"), Version: "head"})
	var a Activation
	if err != nil || json.Unmarshal(r.Value, &a) != nil || !a.Enabled || a.Definition != definition {
		return a, access.ErrForbidden
	}
	return a, nil
}
func (s *Service) Execute(ctx context.Context, j automation.Job) (automation.Result, error) {
	id := "hopper-evaluation:" + hash([]string{j.Workspace, j.Event, j.Target.ID, j.Target.Version, j.Definition.ID, j.Definition.Version})
	var controller automation.Control
	var control Ref
	guard := func(tx pgx.Tx) *apicontract.Problem {
		var err error
		controller, control, err = s.Automation.CheckJobTx(ctx, tx, j)
		if err != nil {
			return problem(err)
		}
		a, err := s.active(ctx, tx, j.Workspace, j.Definition)
		if err == nil && a.Controller != controller.Controller {
			err = access.ErrForbidden
		}
		return problem(err)
	}
	out, err := s.Store.Apply(ctx, persistence.Scope{ActorID: "automation:hopper", Grant: apicontract.Grant{WorkspaceID: j.Workspace, CanRead: true, CanUpdate: true}, Recheck: guard}, persistence.Command{WorkspaceID: j.Workspace, ResourceID: id, Kind: "hopper-evaluate", Mutation: mutation("evaluate:" + id), Payload: raw(struct {
		Event              string
		Target, Definition Ref
	}{j.Event, j.Target, j.Definition})}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		var source Source
		var b []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM hopper_sources WHERE workspace_id=$1 AND event_id=$2 AND target_id=$3 AND target_version=$4`, j.Workspace, j.Event, j.Target.ID, j.Target.Version).Scan(&b); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &source); err != nil {
			return nil, err
		}
		r, err := record(ctx, tx, j.Workspace, j.Definition)
		if err != nil {
			return nil, err
		}
		var d Definition
		_ = json.Unmarshal(r.Value, &d)
		e := Evaluation{Definition: j.Definition, Source: source, Root: source.Root, Depth: source.Depth, State: "evaluated", At: time.Now().UTC()}
		if err = s.evaluate(ctx, tx, j.Workspace, access.Principal{UserID: controller.Controller}, &e, d, Ref{ID: ref.ID, Version: ref.Version}, control, false); err != nil {
			return nil, err
		}
		refs := append(append(append([]Ref{}, e.Source.Refs...), source.Target), append(r.Refs, r.Ref)...)
		if e.Cost != nil {
			refs = append(refs, e.Cost.Sources...)
		}
		saved, err := s.save(ctx, tx, j.Workspace, Ref{ID: ref.ID, Version: ref.Version}, "evaluation", e, refs)
		return raw(saved), err
	})
	if err != nil {
		return automation.Result{}, err
	}
	ref, err := accepted(out, nil)
	if err != nil {
		if out.Error != nil {
			_ = s.Automation.Hold(ctx, j, "hopper_authority_or_basis_changed")
			return automation.Result{}, automation.ErrLease
		}
		return automation.Result{}, err
	}
	return automation.Result{Reason: "hopper_evaluated", Refs: []Ref{ref}, Usage: []Ref{}, UsageState: "not_applicable"}, nil
}
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, w string, p access.Principal, e *Evaluation, d Definition, ref, control Ref, approved bool) error {
	if err := s.refs(ctx, tx, p, w, append(append([]Ref{}, e.Source.Refs...), e.Source.Target)); err != nil {
		return err
	}
	if !e.DryRun {
		if _, err := tx.Exec(ctx, `INSERT INTO hopper_chains(workspace_id,id,bounds) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, w, e.Root, raw(d.Bounds)); err != nil {
			return err
		}
		var b []byte
		var stopped bool
		var evaluations, publications int
		var started time.Time
		if err := tx.QueryRow(ctx, `SELECT bounds,stopped,evaluations,publications,started_at FROM hopper_chains WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, e.Root).Scan(&b, &stopped, &evaluations, &publications, &started); err != nil {
			return err
		}
		var bounds Bounds
		_ = json.Unmarshal(b, &bounds)
		if stopped || e.Depth >= bounds.Depth || publications >= bounds.Publications || evaluations >= bounds.Evaluations || time.Since(started) >= time.Duration(bounds.Seconds)*time.Second {
			e.State = "blocked"
			e.Reason = "chain_limit_or_stopped"
			return nil
		}
		if bounds.Cost != nil {
			cost, err := s.chainCost(ctx, tx, p, w, e.Root, *bounds.Cost)
			if err != nil {
				return err
			}
			e.Cost = &cost
			if cost.State != "observed" {
				e.State = "blocked"
				e.Reason = "cost_" + cost.State
				return nil
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE hopper_chains SET evaluations=evaluations+1 WHERE workspace_id=$1 AND id=$2`, w, e.Root); err != nil {
			return err
		}
	}
	e.Verdict = Evaluate(d.Condition, e.Source.Facts)
	for _, required := range d.Required {
		if required.State == "unfetched" || required.State == "missing" {
			e.State = "input_waiting"
			e.Reason = "required_evidence_unavailable"
			return nil
		}
	}
	if e.Verdict != "true" {
		e.State = "not_matched"
		if e.Verdict == "unknown" {
			e.State = "unknown"
			e.Reason = "result_field_unavailable"
		}
		return nil
	}
	if e.DryRun {
		e.State = "matched"
		e.Reason = "historical_no_publication"
		return nil
	}
	if d.Mode == "proposal" && !approved {
		e.State = "proposal"
		e.Reason = "proposal_policy"
		return nil
	}
	// The normal Workflow owns its configured retries, before Hopper reserves work.
	if e.Source.Kind == "run" && d.Action.Kind == "run" {
		owns, err := s.Workflows.OwnsRetryTx(ctx, tx, w, e.Source.Target, d.Action.Template)
		if err != nil {
			return err
		}
		if owns {
			e.State = "held"
			e.Reason = "workflow_owns_retry"
			return nil
		}
	}
	key := ""
	if problem := d.Problem; problem != nil {
		identity, ok := selected(decodeValue(e.Source.Facts.Report), problem.IdentityPath)
		if !ok {
			e.State = "unknown"
			e.Reason = "problem_identity_missing"
			return nil
		}
		key = hash([]any{problem.Repository, problem.Target, problem.Rule, identity})
		var dest []byte
		var at time.Time
		err := tx.QueryRow(ctx, `SELECT destination,at FROM hopper_problems WHERE workspace_id=$1 AND key=$2`, w, key).Scan(&dest, &at)
		if err != nil && !missing(err) {
			return err
		}
		if err == nil && time.Since(at) < time.Duration(problem.WindowSeconds)*time.Second {
			var previous Ref
			_ = json.Unmarshal(dest, &previous)
			var closed bool
			if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT r.payload->'external'->>'state'='closed' FROM publication_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id=$2 UNION ALL SELECT r.payload->>'state' IN ('succeeded','failed','cancelled') FROM run_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id=$2),false)`, w, previous.ID).Scan(&closed); err != nil {
				return err
			}
			dependencies, err := s.Runs.SourcesTx(ctx, tx, p, w, []Ref{previous})
			if err != nil {
				e.State = "held"
				e.Reason = "duplicate_basis_unavailable"
				return nil
			}
			e.Source.Refs = append(e.Source.Refs, append(dependencies, previous)...)
			e.Destination = &previous
			if problem.Mode == "hold" {
				e.State = "held"
				e.Reason = "duplicate_problem"
				return nil
			}
			if !closed && problem.Mode == "link_open" {
				e.State = "linked"
				e.Reason = "existing_open_issue"
				return nil
			}
			if closed && problem.Mode == "link_open" && !approved {
				e.State = "proposal"
				e.Reason = "recurrence"
				return nil
			}
		}
	}
	publication, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	destination, err := s.publish(ctx, publication, p, w, d, e, ref, control)
	if err != nil {
		_ = publication.Rollback(ctx)
		switch {
		case errors.Is(err, access.ErrForbidden):
			e.State = "blocked"
			e.Reason = "publication_authority_or_input_denied"
		case errors.Is(err, ErrState) || errors.Is(err, runs.ErrState):
			e.State = "input_waiting"
			e.Reason = "mapped_input_unavailable"
		case errors.Is(err, ErrInvalid) || errors.Is(err, runs.ErrInvalid) || errors.Is(err, publications.ErrInvalid) || errors.Is(err, workflows.ErrInvalid):
			e.State = "held"
			e.Reason = "template_input_invalid"
		default:
			return err
		}
		return nil
	}
	if err = publication.Commit(ctx); err != nil {
		return err
	}
	e.Destination = &destination
	e.State = "published"
	e.Reason = ""
	if _, err = tx.Exec(ctx, `UPDATE hopper_chains SET publications=publications+1 WHERE workspace_id=$1 AND id=$2`, w, e.Root); err != nil {
		return err
	}
	if key != "" {
		_, err = tx.Exec(ctx, `INSERT INTO hopper_problems VALUES($1,$2,$3,now()) ON CONFLICT(workspace_id,key) DO UPDATE SET destination=excluded.destination,at=excluded.at`, w, key, raw(destination))
	}
	return err
}
func (s *Service) Step(ctx context.Context, worker string) (bool, error) {
	return s.Automation.RunOne(ctx, "hopper", worker, s.Execute)
}
func (s *Service) Start(ctx context.Context) {
	go func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			worked, err := s.Step(ctx, "hopper-server")
			dispatched, dispatchErr := s.Dispatch(ctx)
			if err == nil && dispatchErr == nil && (worked || dispatched) {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
