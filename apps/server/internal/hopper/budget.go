package hopper

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strings"
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
	runCost, err := usage.ObserveCosts(ctx, tx, p, w, "run", ids, b)
	if err != nil {
		return runCost, err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT a.evaluation_id FROM hopper_model_attempts a JOIN hopper_records i ON i.workspace_id=a.workspace_id AND i.id=a.input_id AND i.version=a.input_version WHERE a.workspace_id=$1 AND i.kind='model-input' AND i.payload#>>'{source,root}'=$2`, w, root)
	if err != nil {
		return runCost, err
	}
	ids = []string{}
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
		return runCost, err
	}
	if len(ids) == 0 {
		return runCost, nil
	}
	modelCost, err := usage.ObserveCosts(ctx, tx, p, w, "hopper", ids, b)
	if err != nil {
		return modelCost, err
	}
	return sumChainCost(b, runCost, modelCost), nil
}

func sumChainCost(b usage.CostBudget, states ...usage.CostState) usage.CostState {
	out := usage.CostState{State: "observed", Currency: b.Currency, Valuation: b.Valuation, Sources: []Ref{}, Guarantee: "further_starts_only"}
	total := new(big.Rat)
	scale := 0
	for _, state := range states {
		out.Sources = append(out.Sources, state.Sources...)
		if state.Amount == nil || (state.State != "observed" && state.State != "exhausted") {
			out.State = "unobserved"
			continue
		}
		value, ok := new(big.Rat).SetString(*state.Amount)
		if !ok {
			out.State = "unobserved"
			continue
		}
		total.Add(total, value)
		_, fraction, _ := strings.Cut(*state.Amount, ".")
		if len(fraction) > scale {
			scale = len(fraction)
		}
	}
	if out.State == "observed" {
		text := total.FloatString(scale)
		out.Amount = &text
		limit, _ := new(big.Rat).SetString(b.Amount)
		if total.Cmp(limit) >= 0 {
			out.State = "exhausted"
		}
	}
	return out
}
