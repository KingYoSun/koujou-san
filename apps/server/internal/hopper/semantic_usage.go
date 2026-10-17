package hopper

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
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
	quantities := reply.Quantities()
	request, coverage, state := "", "unknown", "provisional"
	var actual *string
	if !provisional {
		state = "final"
		request = reply.ID
		if reply.Model != "" {
			actual = &reply.Model
		}
		if response != nil && response.RequestID != "" {
			request = response.RequestID
		}
		if request != "" {
			coverage = "confirmed"
		}
		var extra struct {
			Extra struct {
				Routing struct {
					Fallback bool `json:"is_fallback"`
				} `json:"routing_info"`
			} `json:"extra_fields"`
		}
		if response != nil {
			_ = json.Unmarshal(response.Body, &extra)
		}
		if extra.Extra.Routing.Fallback {
			state = "partial"
			coverage = "unknown"
			// The final provider's usage does not report earlier fallback attempts.
			// Preserve it in the raw response, while the total remains unmeasured.
			for k := range quantities {
				quantities[k] = modelreply.Quantity(nil, nil)
			}
		}
		if reply.Usage.Input == nil || reply.Usage.Output == nil {
			state = "unavailable"
		}
	}
	observation := usage.Observation{Source: "server-model:" + d.Semantic.Connection, Transform: "server-chat-v1", Event: intent.ID, Series: intent.ID, Mode: "delta", Group: intent.ID, Request: request, Covers: []string{}, Coverage: coverage, Connection: d.Semantic.Connection, Contract: d.Semantic.Contract, RequestedModel: d.Semantic.Model, ActualModel: actual, At: time.Now().UTC(), Attribution: usage.Attribution{Kind: "hopper", ID: evaluation, Resource: input.ID, Scopes: []string{}}, Quantities: quantities, Measurement: &usage.Measurement{State: state, Protocol: "openai-chat-completions", Scope: "hopper_evaluation_request", Generation: intent.ID, Billing: "unknown", SourceRef: intent.ID, SourceDigest: hash(response)}}
	id := usage.ObservationID(observation)
	var expected string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')`, w, id).Scan(&expected); err != nil {
		return nil, nil, err
	}
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
	observed, err := s.Usage.PutFromOwner(ctx, p, w, id, usage.Write{Mutation: apicontract.Mutation{OperationID: semanticAttemptOperation(intent.ID, provisional), ExpectedVersion: expected}, Value: raw(observation)}, guard, tx)
	ref, err := accepted(observed, err)
	if err != nil {
		return nil, nil, err
	}
	refs := []Ref{ref}
	costs := []Ref{}
	if actual != nil {
		if price, ok := d.Semantic.Prices[*actual]; ok {
			valuation := d.Semantic.Valuation
			if valuation == "" {
				valuation = "estimated"
			}
			nested, err := tx.Begin(ctx)
			if err != nil {
				return nil, nil, err
			}
			cost, err := accepted(s.Usage.Put(ctx, p, w, "evaluation:"+hash([]string{intent.ID, valuation}), usage.Write{Mutation: mutation("hopper-price:" + intent.ID), Value: raw(usage.Evaluation{Group: observation.Group, Kind: valuation, Usage: []usage.Ref{{ID: ref.ID, Version: ref.Version}}, Price: &price})}, nested))
			if err != nil {
				_ = nested.Rollback(ctx)
				if !errors.Is(err, usage.ErrInvalid) && !errors.Is(err, access.ErrForbidden) && !errors.Is(err, ErrState) {
					return nil, nil, err
				}
			} else {
				if err = nested.Commit(ctx); err != nil {
					return nil, nil, err
				}
				costs = append(costs, cost)
			}
		}
	}
	return refs, costs, nil
}
