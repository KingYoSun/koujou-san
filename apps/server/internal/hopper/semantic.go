package hopper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/jackc/pgx/v5"
)

type modelAttempt struct {
	Evaluation string              `json:"evaluation"`
	Input      Ref                 `json:"input"`
	Number     int                 `json:"number"`
	Actor      string              `json:"actor"`
	Response   *serverllm.Response `json:"response,omitempty"`
	Error      string              `json:"error,omitempty"`
	Outcome    *SemanticOutcome    `json:"outcome,omitempty"`
}

func (s *Service) semantic(ctx context.Context, j automation.Job, evaluation string, d Definition, source Source) (result *SemanticOutcome, resultErr error) {
	defer func() {
		if result != nil && resultErr == nil {
			resultErr = s.semanticUsage(ctx, j.Workspace, evaluation, result)
		}
	}()
	if !hasSemantic(d.Condition) || Evaluate(d.Condition, source.Facts) != "unknown" {
		return nil, nil
	}
	preflight := Evaluation{Source: source, Root: source.Root, Depth: source.Depth, DryRun: source.DryRun}
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		actor, _, err := s.checkSemanticJob(ctx, tx, j, source)
		if err != nil {
			return err
		}
		return s.checkChain(ctx, tx, j.Workspace, actor, &preflight, d, false)
	})
	if err != nil {
		return nil, err
	}
	if preflight.State == "blocked" {
		return &SemanticOutcome{State: preflight.State, Reason: preflight.Reason, Usage: []Ref{}, Costs: []Ref{}, UsageState: "not_applicable"}, nil
	}
	for _, required := range d.Required {
		if required.State == "unfetched" || required.State == "missing" {
			return &SemanticOutcome{State: "input_waiting", Reason: "required_evidence_unavailable", Usage: []Ref{}, Costs: []Ref{}, UsageState: "not_applicable"}, nil
		}
	}
	prepared, input, err := s.prepareSemanticInput(ctx, j, evaluation, d, source)
	if err != nil {
		return nil, err
	}
	outcome := &SemanticOutcome{Input: &prepared.Ref, State: input.State, Reason: input.Reason, Usage: []Ref{}, Costs: []Ref{}, UsageState: "not_applicable"}
	if input.State != "ready" {
		return outcome, nil
	}
	var lastID, lastVersion string
	err = s.Store.Database().QueryRow(ctx, `SELECT resource_id,COALESCE(response_version,'') FROM hopper_model_attempts WHERE workspace_id=$1 AND evaluation_id=$2 ORDER BY number DESC LIMIT 1`, j.Workspace, evaluation).Scan(&lastID, &lastVersion)
	if err != nil && !missing(err) {
		return nil, err
	}
	if err == nil && lastVersion != "" {
		r, err := record(ctx, s.Store.Database(), j.Workspace, Ref{ID: lastID, Version: lastVersion})
		if err != nil {
			return nil, err
		}
		var previous modelAttempt
		if json.Unmarshal(r.Value, &previous) != nil {
			return nil, ErrInvalid
		}
		if previous.Outcome != nil && previous.Outcome.State != "failed" {
			return previous.Outcome, nil
		}
	}
	connection, err := s.Gateway.Connection(j.Workspace, d.Semantic.Connection, d.Semantic.Revision)
	if err != nil {
		outcome.State = "blocked"
		outcome.Reason = "model_connection_unavailable"
		return outcome, nil
	}
	if !connection.AllowsChain(d.Semantic.Model, d.Semantic.Fallbacks, d.Semantic.Allowed) {
		outcome.State = "blocked"
		outcome.Reason = "model_not_permitted"
		return outcome, nil
	}
	var actor access.Principal
	var maxAttempts int
	check := func(tx pgx.Tx) error {
		var err error
		actor, _, err = s.checkSemanticJob(ctx, tx, j, source)
		if err != nil {
			return err
		}
		reader := actor
		reader.OperationID = input.Operation
		if err = s.refs(ctx, tx, reader, j.Workspace, prepared.Refs); err != nil {
			return err
		}
		var config []byte
		if err = tx.QueryRow(ctx, `SELECT r.payload FROM automation_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id=$1 AND r.id='control:hopper'`, j.Workspace).Scan(&config); err != nil {
			return err
		}
		var control automation.Control
		if json.Unmarshal(config, &control) != nil {
			return ErrInvalid
		}
		maxAttempts = control.MaxAttempts
		if err = s.checkChain(ctx, tx, j.Workspace, actor, &preflight, d, false); err != nil {
			return err
		}
		if preflight.State == "blocked" {
			return ErrState
		}
		if d.Semantic.Cost != nil {
			cost, err := s.semanticCost(ctx, tx, actor, j.Workspace, evaluation, *d.Semantic.Cost)
			if err != nil {
				return err
			}
			if cost.State != "observed" {
				return access.ErrForbidden
			}
		}
		return nil
	}
	number := j.Attempts
	id := "hopper-model:" + hash([]any{evaluation, number})
	var made bool
	attempt := modelAttempt{Evaluation: evaluation, Input: prepared.Ref, Number: number}
	intent, err := s.system(ctx, j.Workspace, id, "model-intent", "intent:"+id, "0", check, func(tx pgx.Tx, ref Ref) (Record, error) {
		attempt.Actor = actor.UserID
		tag, err := tx.Exec(ctx, `INSERT INTO hopper_model_attempts(workspace_id,evaluation_id,number,resource_id,input_id,input_version,job_id,lease,actor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, j.Workspace, evaluation, number, id, prepared.ID, prepared.Version, j.ID, j.Lease, actor.UserID)
		if err != nil {
			return Record{}, err
		}
		made = tag.RowsAffected() == 1
		if !made {
			return Record{}, ErrState
		}
		r, err := s.save(ctx, tx, j.Workspace, ref, "model-attempt", attempt, append(append([]Ref{}, prepared.Refs...), prepared.Ref))
		if err == nil {
			_, _, err = s.observeSemantic(ctx, tx, actor, j.Workspace, d, evaluation, ref, prepared, modelreply.Reply{}, nil, true)
		}
		return r, err
	})
	if err != nil {
		if preflight.State == "blocked" {
			outcome.State, outcome.Reason = preflight.State, preflight.Reason
			return outcome, nil
		}
		return nil, err
	}
	if !made {
		return nil, ErrState
	}
	err = serverllm.Dispatch(ctx, connection, d.Semantic, input.Request, id, func(saveCtx context.Context, response serverllm.Response, reply modelreply.Reply, reason string) error {
		attempt.Response = &response
		if reason == "model_request_failed" {
			attempt.Error = "model_connection_unavailable"
		}
		outcome.State = "complete"
		outcome.Reason = ""
		if reason == "model_request_failed" {
			outcome.State = "failed"
			outcome.Reason = "model_request_failed"
		} else if reason != "" {
			outcome.State = "failed"
			outcome.Reason = reason
		} else {
			var decision SemanticDecision
			if len(reply.Choices[0].Message.Calls) > 0 || reply.Choices[0].Message.Content == nil || json.Unmarshal([]byte(*reply.Choices[0].Message.Content), &decision) != nil || !decision.validate(input) {
				outcome.State = "failed"
				outcome.Reason = "invalid_semantic_response"
			} else {
				outcome.Decision = &decision
				if decision.Verdict == "unknown" || len(decision.Unknown) > 0 {
					outcome.State = "unknown"
					outcome.Reason = decision.Reason
				}
			}
		}
		reserved := func(tx pgx.Tx) error {
			var exists bool
			err := tx.QueryRow(saveCtx, `SELECT EXISTS(SELECT 1 FROM hopper_model_attempts WHERE workspace_id=$1 AND resource_id=$2 AND job_id=$3 AND lease=$4 AND input_id=$5 AND input_version=$6 AND actor=$7)`, j.Workspace, id, j.ID, j.Lease, prepared.ID, prepared.Version, attempt.Actor).Scan(&exists)
			if err == nil && !exists {
				return access.ErrForbidden
			}
			return err
		}
		responseRef, err := s.system(saveCtx, j.Workspace, id, "model-response", "response:"+id, intent.Version, reserved, func(tx pgx.Tx, ref Ref) (Record, error) {
			outcome.Response = &ref
			usage, costs, err := s.observeSemantic(saveCtx, tx, actor, j.Workspace, d, evaluation, intent, prepared, reply, &response, false)
			if err != nil {
				return Record{}, err
			}
			outcome.Usage = usage
			outcome.Costs = costs
			outcome.UsageState = "missing"
			if len(costs) > 0 {
				var rated bool
				if err = tx.QueryRow(saveCtx, `SELECT payload->>'state'='rated' FROM usage_records WHERE workspace_id=$1 AND id=$2 AND version=$3`, j.Workspace, costs[0].ID, costs[0].Version).Scan(&rated); err != nil {
					return Record{}, err
				}
				if rated {
					outcome.UsageState = "complete"
				}
			}
			attempt.Outcome = outcome
			refs := append(append(append([]Ref{}, prepared.Refs...), prepared.Ref), append(usage, costs...)...)
			r, err := s.save(saveCtx, tx, j.Workspace, ref, "model-attempt", attempt, refs)
			if err == nil {
				_, err = tx.Exec(saveCtx, `UPDATE hopper_model_attempts SET response_version=$3,state='observed' WHERE workspace_id=$1 AND resource_id=$2`, j.Workspace, id, ref.Version)
			}
			return r, err
		})
		if err != nil {
			return err
		}
		outcome.Response = &responseRef
		return nil
	})
	if err != nil {
		return nil, err
	}
	if outcome.State == "failed" && j.Attempts < maxAttempts {
		return nil, errors.New("retry_semantic_request")
	}
	return outcome, nil
}

func (s *Service) recheckSemantic(ctx context.Context, tx pgx.Tx, p access.Principal, w string, e *Evaluation, d Definition) error {
	if e.Semantic == nil || e.Semantic.Input == nil {
		return nil
	}
	r, err := record(ctx, tx, w, *e.Semantic.Input)
	if err != nil {
		return err
	}
	var input SemanticInput
	if json.Unmarshal(r.Value, &input) != nil {
		return ErrInvalid
	}
	reader := p
	reader.OperationID = input.Operation
	if err = s.refs(ctx, tx, reader, w, r.Refs); err != nil {
		return err
	}
	if e.Semantic.State == "complete" || e.Semantic.State == "unknown" {
		if _, err = s.Gateway.Connection(w, d.Semantic.Connection, d.Semantic.Revision); err != nil {
			return err
		}
	}
	e.protected = append(e.protected, r.Refs...)
	e.audit = append(append(e.audit, r.Refs...), r.Ref)
	if e.Semantic.Response != nil {
		e.audit = append(e.audit, *e.Semantic.Response)
	}
	e.audit = append(e.audit, append(e.Semantic.Usage, e.Semantic.Costs...)...)
	return nil
}

func semanticEvaluationID(j automation.Job) string {
	return "hopper-evaluation:" + hash([]string{j.Workspace, j.Event, j.Target.ID, j.Target.Version, j.Definition.ID, j.Definition.Version})
}

func semanticAttemptOperation(id string, provisional bool) string {
	return fmt.Sprintf("hopper-usage:%s:%t", id, provisional)
}
