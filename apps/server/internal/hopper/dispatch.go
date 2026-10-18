package hopper

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

// Dispatch reserves one already-published Run for normal assignment. No CLI is
// launched here; an absent Runner leaves the same durable Run waiting.
func (s *Service) Dispatch(ctx context.Context) (bool, error) {
	worked := false
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		var w, id, controller, preferred string
		var definitionRaw, repositoryRaw, requiredRaw []byte
		err := tx.QueryRow(ctx, `SELECT d.workspace_id,d.run_id,d.controller,d.preferred_device,d.definition,d.repository,d.required FROM hopper_dispatch d LEFT JOIN triage_run_links l ON l.workspace_id=d.workspace_id AND l.run_id=d.run_id LEFT JOIN triage_ranks p ON p.workspace_id=l.workspace_id AND p.scope_id=l.scope_id WHERE d.state='waiting' AND d.due_at<=now() ORDER BY COALESCE(p.policy_id,''),COALESCE(p.policy_version,''),p.rank NULLS LAST,l.received_at,d.due_at,d.workspace_id,d.run_id LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(&w, &id, &controller, &preferred, &definitionRaw, &repositoryRaw, &requiredRaw)
		if missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		update := func(state, reason string) error {
			_, err := tx.Exec(ctx, `UPDATE hopper_dispatch SET state=$3,reason=$4,due_at=now()+interval '5 seconds' WHERE workspace_id=$1 AND run_id=$2`, w, id, state, reason)
			return err
		}
		var definition Ref
		var repository *runs.Repository
		var required []string
		if json.Unmarshal(definitionRaw, &definition) != nil || json.Unmarshal(repositoryRaw, &repository) != nil || json.Unmarshal(requiredRaw, &required) != nil {
			return ErrInvalid
		}
		var version string
		var payload, refsRaw []byte
		if err = tx.QueryRow(ctx, `SELECT r.version,r.payload,r.refs FROM run_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id=$2`, w, id).Scan(&version, &payload, &refsRaw); err != nil {
			return err
		}
		var run runs.Run
		var refs []Ref
		_ = json.Unmarshal(payload, &run)
		_ = json.Unmarshal(refsRaw, &refs)
		if run.Assignment != nil || run.State != "queued" || run.CancelRequested || run.StartedAt != nil {
			return update("finished", "run_already_assigned_or_ended")
		}
		a, err := s.active(ctx, tx, w, definition)
		if err != nil || a.Controller != controller {
			return update("held", "definition_or_controller_changed")
		}
		c, _, err := s.Automation.CheckManualTx(ctx, tx, w, refs)
		if err != nil || c.Controller != controller {
			return update("held", "automation_authority_or_budget")
		}
		var stopped bool
		var root string
		var boundsRaw []byte
		if err = tx.QueryRow(ctx, `SELECT c.stopped OR c.started_at+make_interval(secs=>(c.bounds->>'seconds')::int)<=now(),c.id,c.bounds FROM hopper_lineage l JOIN hopper_chains c ON c.workspace_id=l.workspace_id AND c.id=l.root WHERE l.workspace_id=$1 AND l.resource_id=$2`, w, id).Scan(&stopped, &root, &boundsRaw); err != nil {
			return err
		}
		if stopped {
			return update("held", "chain_stopped_or_expired")
		}
		var bounds Bounds
		_ = json.Unmarshal(boundsRaw, &bounds)
		if bounds.Cost != nil {
			cost, err := s.chainCost(ctx, tx, access.Principal{UserID: controller}, w, root, *bounds.Cost)
			if err != nil {
				return err
			}
			if cost.State != "observed" {
				return update("held", "cost_"+cost.State)
			}
		}
		if reason, err := s.Automation.AssignmentGate(ctx, tx, w, id); err != nil {
			return err
		} else if reason != "" {
			return update("waiting", reason)
		}
		actor := access.Principal{UserID: controller}
		runner := s.Runs.InTransaction(tx)
		device, err := runner.SelectRunner(ctx, actor, w, id, preferred, repository, required)
		if err != nil {
			return err
		}
		if device == "" {
			return update("waiting", "runner_unavailable")
		}
		_, err = accepted(runner.Assign(ctx, actor, w, id, runs.Assign{Mutation: structMutation("hopper-assign:"+id+":"+device+":"+version, version), DeviceID: device, Repository: repository, Required: required}))
		if err != nil {
			return err
		}
		var currentRaw []byte
		if err = tx.QueryRow(ctx, `SELECT r.payload FROM run_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id=$2`, w, id).Scan(&currentRaw); err != nil {
			return err
		}
		if json.Unmarshal(currentRaw, &run) != nil {
			return ErrInvalid
		}
		if run.Assignment == nil {
			return update("waiting", run.Preparation)
		}
		return update("assigned", "")
	})
	return worked, err
}
