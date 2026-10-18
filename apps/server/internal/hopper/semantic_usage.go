package hopper

import (
	"context"
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

func (s *Service) semanticCost(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string, b usage.CostBudget) (usage.CostState, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM usage_entries WHERE workspace_id=$1 AND attribution_kind='hopper' AND attribution_id=$2)`, w, id).Scan(&exists); err != nil {
		return usage.CostState{}, err
	}
	ids := []string{}
	if exists {
		ids = append(ids, id)
	}
	return usage.ObserveCosts(ctx, tx, p, w, "hopper", ids, b)
}

// The adopted evaluation owns every attempt, including unobserved reservations.
// Reusing a saved response must retain earlier retries' consumption as well.
func (s *Service) semanticUsage(ctx context.Context, w, evaluation string, outcome *SemanticOutcome) error {
	rows, err := s.Store.Database().Query(ctx, `SELECT r.payload,u.resource_id,h.version FROM hopper_model_attempts a LEFT JOIN hopper_records r ON r.workspace_id=a.workspace_id AND r.id=a.resource_id AND r.version=a.response_version LEFT JOIN usage_events u ON u.workspace_id=a.workspace_id AND u.event_id=a.resource_id AND u.source LIKE 'server-model:%' LEFT JOIN shared_versions h ON h.workspace_id=u.workspace_id AND h.resource_id=u.resource_id WHERE a.workspace_id=$1 AND a.evaluation_id=$2 ORDER BY a.number`, w, evaluation)
	if err != nil {
		return err
	}
	defer rows.Close()
	outcome.Usage, outcome.Costs, outcome.UsageState = []Ref{}, []Ref{}, "not_applicable"
	for rows.Next() {
		if outcome.UsageState == "not_applicable" {
			outcome.UsageState = "complete"
		}
		var payload []byte
		var id, version *string
		if err = rows.Scan(&payload, &id, &version); err != nil {
			return err
		}
		var attempt modelAttempt
		if len(payload) == 0 || json.Unmarshal(payload, &attempt) != nil || attempt.Outcome == nil || attempt.Outcome.UsageState != "complete" {
			outcome.UsageState = "missing"
		}
		if id != nil && version != nil {
			outcome.Usage = append(outcome.Usage, Ref{ID: *id, Version: *version})
		}
		if attempt.Outcome != nil {
			outcome.Costs = append(outcome.Costs, attempt.Outcome.Costs...)
		}
	}
	return rows.Err()
}

func (s *Service) observeSemantic(ctx context.Context, tx pgx.Tx, p access.Principal, w string, d Definition, evaluation string, intent Ref, input Record, reply modelreply.Reply, response *serverllm.Response, provisional bool) ([]Ref, []Ref, error) {
	deps := []usage.Ref{}
	for _, ref := range append(append([]Ref{}, input.Refs...), input.Ref, intent) {
		deps = append(deps, usage.Ref{ID: ref.ID, Version: ref.Version})
	}
	guard := usage.OwnerWrite{Actor: "automation:hopper", Dependencies: deps, Reserved: !provisional, Check: func(ctx context.Context, tx pgx.Tx) error {
		var present bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hopper_model_attempts WHERE workspace_id=$1 AND resource_id=$2 AND input_id=$3 AND input_version=$4 AND actor=$5)`, w, intent.ID, input.ID, input.Version, p.UserID).Scan(&present)
		if err == nil && !present {
			return access.ErrForbidden
		}
		return err
	}}
	return serverllm.Observe(ctx, tx, p, w, "hopper", evaluation, semanticAttemptOperation(intent.ID, provisional), intent, serverllm.MeterInput{ID: input.ID, Version: input.Version, Refs: input.Refs}, d.Semantic, reply, response, provisional, s.Usage, guard)
}
