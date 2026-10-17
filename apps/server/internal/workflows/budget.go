package workflows

import (
	"context"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

type CostState = usage.CostState

func (s *Service) cost(ctx context.Context, q query, p access.Principal, w, id string, b CostBudget) (CostState, error) {
	out := CostState{}
	rows, err := q.Query(ctx, `SELECT t.run_id FROM workflow_tasks t JOIN shared_versions v ON v.workspace_id=t.workspace_id AND v.resource_id=t.run_id JOIN run_records r ON r.workspace_id=v.workspace_id AND r.id=v.resource_id AND r.version=v.version JOIN workflow_records d ON d.workspace_id=t.workspace_id AND d.id=t.payload->'definition'->>'id' AND d.version=t.payload->'definition'->>'version' WHERE t.workspace_id=$1 AND t.execution_id=$2 AND r.payload->>'started_at' IS NOT NULL AND d.payload->'ci' IS NULL`, w, id)
	if err != nil {
		return out, err
	}
	required := map[string]bool{}
	for rows.Next() {
		var run string
		if err = rows.Scan(&run); err != nil {
			rows.Close()
			return out, err
		}
		required[run] = false
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	ids := []string{}
	for id := range required {
		ids = append(ids, id)
	}
	return usage.ObserveCosts(ctx, q, p, w, "run", ids, usage.CostBudget(b))
}
