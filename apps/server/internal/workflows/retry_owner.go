package workflows

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

func runOutcome(run runs.Run, stale bool) string {
	outcome := run.State
	if run.Assessment != nil {
		if run.Assessment.State != "valid" || run.State != "succeeded" {
			outcome = "INCONCLUSIVE"
		} else if run.Assessment.Verdict != "" {
			outcome = run.Assessment.Verdict
		}
	}
	if stale {
		return "STALE"
	}
	return outcome
}

// OwnsRetryTx compares the actual outcome and retry destination, rather than
// suppressing unrelated Hopper actions merely because a Workflow has retries.
func (s *Service) OwnsRetryTx(ctx context.Context, tx pgx.Tx, w string, source, template Ref) (bool, error) {
	var taskRaw, executionRaw, definitionRaw, runRaw []byte
	var superseded bool
	err := tx.QueryRow(ctx, `SELECT t.payload,e.payload,d.payload,r.payload,EXISTS(SELECT 1 FROM workflow_tasks later WHERE later.workspace_id=t.workspace_id AND later.execution_id=t.execution_id AND later.node_id=t.node_id AND later.attempt>t.attempt) FROM workflow_tasks t JOIN workflow_records e ON e.workspace_id=t.workspace_id AND e.id=t.execution_id JOIN shared_versions h ON h.workspace_id=e.workspace_id AND h.resource_id=e.id AND h.version=e.version JOIN workflow_records d ON d.workspace_id=e.workspace_id AND d.id=e.payload->'definition'->>'id' AND d.version=e.payload->'definition'->>'version' JOIN run_records r ON r.workspace_id=t.workspace_id AND r.id=t.run_id AND r.version=$3 WHERE t.workspace_id=$1 AND t.run_id=$2`, w, source.ID, source.Version).Scan(&taskRaw, &executionRaw, &definitionRaw, &runRaw, &superseded)
	if notFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var task Task
	var execution Execution
	var definition Definition
	var run runs.Run
	for _, v := range []struct {
		b []byte
		v any
	}{{taskRaw, &task}, {executionRaw, &execution}, {definitionRaw, &definition}, {runRaw, &run}} {
		if err = json.Unmarshal(v.b, v.v); err != nil {
			return false, err
		}
	}
	if execution.State != "running" && execution.State != "waiting" && !task.Retried && !superseded {
		return false, nil
	}
	next := ""
	for _, n := range definition.Nodes {
		if n.ID == task.Node {
			next = n.Retry[runOutcome(run, task.Stale)]
		}
	}
	if next == "" {
		return false, nil
	}
	for _, n := range definition.Nodes {
		if n.ID == next && n.Definition != nil && *n.Definition == template {
			return true, nil
		}
	}
	return false, nil
}
