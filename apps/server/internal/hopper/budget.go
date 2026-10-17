package hopper

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

func (s *Service) chainCost(ctx context.Context, tx pgx.Tx, p access.Principal, w, root string, b usage.CostBudget) (usage.CostState, error) {
	rows, err := tx.Query(ctx, `SELECT l.resource_id FROM hopper_lineage l JOIN shared_versions h ON h.workspace_id=l.workspace_id AND h.resource_id=l.resource_id JOIN run_records r ON r.workspace_id=h.workspace_id AND r.id=h.resource_id AND r.version=h.version JOIN run_records i ON i.workspace_id=r.workspace_id AND i.id=r.payload->'input'->>'id' AND i.version=r.payload->'input'->>'version' WHERE l.workspace_id=$1 AND l.root=$2 AND l.resource_id LIKE 'run:%' AND r.payload->>'started_at' IS NOT NULL AND i.payload->'input'->'command'->>'kind' NOT IN ('server-connector','server-observer')`, w, root)
	if err != nil {
		return usage.CostState{}, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return usage.CostState{}, err
	}
	return usage.ObserveCosts(ctx, tx, p, w, "run", ids, b)
}
