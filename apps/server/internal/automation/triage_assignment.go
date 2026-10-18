package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

func (s *Service) assignmentAccepted(ctx context.Context, tx pgx.Tx, c persistence.Command, ref apicontract.Reference, state json.RawMessage) error {
	w := c.WorkspaceID
	if strings.HasPrefix(ref.ID, "priority:scope:") {
		var rec Record
		var priority Priority
		if json.Unmarshal(state, &rec) != nil || json.Unmarshal(rec.Value, &priority) != nil {
			return nil
		}
		if priority.Adopted == nil {
			_, err := tx.Exec(ctx, `DELETE FROM triage_ranks WHERE workspace_id=$1 AND scope_id=$2`, w, priority.Target.ID)
			return err
		}
		policyRef := priority.Policy
		if priority.AdoptedPolicy != nil {
			policyRef = *priority.AdoptedPolicy
		}
		rec, err := record(ctx, tx, w, policyRef)
		if err != nil {
			return err
		}
		var p Policy
		_ = json.Unmarshal(rec.Value, &p)
		for rank, level := range p.Levels {
			if level.ID == *priority.Adopted {
				_, err = tx.Exec(ctx, `INSERT INTO triage_ranks VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,scope_id) DO UPDATE SET policy_id=excluded.policy_id,policy_version=excluded.policy_version,rank=excluded.rank`, w, priority.Target.ID, policyRef.ID, policyRef.Version, rank)
				return err
			}
		}
		return ErrInvalid
	}
	if c.Kind == "run-create" {
		var rec struct {
			Value runs.Run `json:"value"`
		}
		if json.Unmarshal(state, &rec) != nil {
			return ErrInvalid
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='input'`, w, rec.Value.Input.ID, rec.Value.Input.Version).Scan(&raw); err != nil {
			return err
		}
		var spec runs.Spec
		if json.Unmarshal(raw, &spec) != nil {
			return ErrInvalid
		}
		if spec.Issue != nil && spec.Input.Command.Kind == "cli" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2)`, w, spec.Issue.ID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalid
			}
			_, err := tx.Exec(ctx, `INSERT INTO triage_run_links(workspace_id,run_id,scope_id) VALUES($1,$2,$3)`, w, ref.ID, spec.Issue.ID)
			return err
		}
	}
	return nil
}

func (s *Service) AttachRuns(r *runs.Service) {
	if r == nil {
		return
	}
	previous := r.AssignmentGate
	r.AssignmentGate = func(ctx context.Context, tx pgx.Tx, w, id string) (string, error) {
		if previous != nil {
			reason, err := previous(ctx, tx, w, id)
			if err != nil || reason != "" {
				return reason, err
			}
		}
		return s.AssignmentGate(ctx, tx, w, id)
	}
}

func (s *Service) AssignmentGate(ctx context.Context, tx pgx.Tx, w, run string) (string, error) {
	var scope string
	err := tx.QueryRow(ctx, `SELECT scope_id FROM triage_run_links WHERE workspace_id=$1 AND run_id=$2`, w, run).Scan(&scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_ranks WHERE workspace_id=$1 AND scope_id=$2)`, w, scope).Scan(&exists); err != nil {
		return "", err
	}
	if !exists {
		return "triage_priority_waiting", nil
	}
	var id, version string
	if err = tx.QueryRow(ctx, `SELECT info_id,info_version FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2`, w, scope).Scan(&id, &version); err != nil {
		return "", err
	}
	if id == "" {
		return "", nil
	}
	rec, err := record(ctx, tx, w, Ref{ID: id, Version: version})
	if err != nil {
		return "", err
	}
	var info Issue
	if json.Unmarshal(rec.Value, &info) != nil {
		return "", ErrInvalid
	}
	state := info.State
	if info.Observed != nil {
		state = info.Observed.State
	}
	if state != "open" {
		return "issue_closed", nil
	}
	for _, dependency := range info.Dependencies {
		if strings.HasPrefix(dependency.ID, "scope:") {
			var raw []byte
			err = tx.QueryRow(ctx, `SELECT r.payload FROM triage_targets t JOIN automation_records r ON r.workspace_id=t.workspace_id AND r.id=t.info_id AND r.version=t.info_version WHERE t.workspace_id=$1 AND t.scope_id=$2`, w, dependency.ID).Scan(&raw)
			var related Issue
			if err != nil || json.Unmarshal(raw, &related) != nil {
				return "issue_dependency_waiting", nil
			}
			state := related.State
			if related.Observed != nil {
				state = related.Observed.State
			}
			if state != "closed" {
				return "issue_dependency_waiting", nil
			}
		} else if strings.HasPrefix(dependency.ID, "run:") {
			var done bool
			err = tx.QueryRow(ctx, `SELECT r.payload->>'state'='succeeded' FROM run_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id=$2`, w, dependency.ID).Scan(&done)
			if err != nil || !done {
				return "issue_dependency_waiting", nil
			}
		} else {
			return "issue_dependency_waiting", nil
		}
	}
	return "", nil
}
