package clientops

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

type Reply = modelreply.Reply

func ReadTool(name string, purpose ...string) bool {
	return (len(purpose) > 0 && purpose[0] == "context-generation" && name == "run_get") || name == "context_search" || name == "context_graph" || name == "context_get" || name == "context_resolve" || (len(purpose) > 0 && purpose[0] == "definition" && (name == "workflow_get" || name == "workflow_query" || name == "workflow_capabilities"))
}
func parseReply(b json.RawMessage, m Model, purpose ...string) (Reply, string) {
	r, reason := modelreply.Parse(b, m.Allowed)
	if reason != "" && reason != "incomplete_model_response" {
		return r, reason
	}
	for _, call := range r.Choices[0].Message.Calls {
		if call.ID == "" || call.Type != "function" || !ReadTool(call.Function.Name, purpose...) || !json.Valid([]byte(call.Function.Arguments)) {
			return r, "tool_not_allowed"
		}
	}
	return r, reason
}

type Finish struct {
	apicontract.Mutation
	Client   string          `json:"client"`
	Turn     Ref             `json:"turn"`
	Response json.RawMessage `json:"response"`
	Failure  string          `json:"failure,omitempty"`
	Status   int             `json:"http_status,omitempty"`
}

func (s *Service) Finish(ctx context.Context, p access.Principal, w, id string, req Finish) (apicontract.Response[apicontract.Reference], error) {
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, r Ref) (Record, error) {
		old, op, err := s.owner(ctx, tx, p, w, id)
		if err != nil {
			return Record{}, err
		}
		if op.State != "sending" || op.Turn == nil || *op.Turn != req.Turn {
			return Record{}, ErrState
		}
		intent, err := record(ctx, tx, w, req.Turn)
		if err != nil {
			return Record{}, err
		}
		var turn Turn
		if decode(intent.Value, &turn) != nil || turn.State != "intent" || turn.Client != req.Client {
			return Record{}, access.ErrForbidden
		}
		turn.Response = req.Response
		turn.Status = req.Status
		turn.Failure = req.Failure
		turn.State = "response"
		result, reason := parseReply(req.Response, turn.Model, op.Purpose)
		if req.Failure != "" {
			reason = "model_connection_failed"
		}
		deps := unique(append(append([]Ref{}, old.Refs...), intent.Refs...))
		observedRefs, err := s.mcpSources(ctx, tx, w, op.ReadOperation, nil, false)
		if err != nil {
			return Record{}, err
		}
		deps = unique(append(deps, observedRefs...))
		pin, err := accepted(s.mutate(ctx, p, w, intent.ID, apicontract.Mutation{OperationID: "response:" + intent.ID, ExpectedVersion: intent.Version}, turn, func(tx pgx.Tx, ref Ref) (Record, error) { return s.save(ctx, tx, w, p, ref, "turn", turn, deps, id) }, tx))
		if err != nil {
			return Record{}, err
		}
		op.Turn = &pin
		if err = s.observe(ctx, tx, p, w, old.Ref, op, turn, intent, result, reason == "model_connection_failed" || reason == "model_not_allowed" || reason == "invalid_model_response", false); err != nil {
			return Record{}, err
		}
		policy, policyErr := s.currentPolicy(ctx, tx, p, w, op)
		if policyErr == nil {
			for _, budget := range budgets(policy, op.Model) {
				cost, err := usage.ObserveCosts(ctx, tx, p, w, "operation", []string{id}, budget)
				if err != nil {
					return Record{}, err
				}
				op.Cost = &cost
				deps = unique(append(deps, cost.Sources...))
				if cost.State != "observed" {
					break
				}
			}
		}
		op.State = "failed"
		op.Reason = reason
		if reason == "" {
			if len(result.Choices[0].Message.Calls) > 0 {
				op.State = "ready"
			} else {
				var proposal Proposal
				if result.Choices[0].Message.Content == nil || decodeOperationProposal([]byte(*result.Choices[0].Message.Content), op.Purpose, &proposal) != nil {
					op.Reason = "invalid_selection_response"
				} else {
					proposalRef, err := s.writeRelated(ctx, tx, p, w, "client-proposal:"+r.Version, "proposal", proposal, append(deps, pin), id)
					if err != nil {
						return Record{}, err
					}
					op.Proposal = &proposalRef
					op.State = "proposal"
					if len(proposal.Unknown) > 0 {
						op.State = "incomplete"
					}
					if len(proposal.Questions) > 0 {
						op.State = "question"
					}
					if op.Client != req.Client {
						op.State = "proposal"
						op.Reason = "client_changed"
					} else if complete(proposal) {
						if policyErr != nil {
							op.Reason = "policy_changed"
						} else if policy.Mode == "auto" {
							var expired bool
							if err = tx.QueryRow(ctx, `SELECT deadline<=now() FROM client_operation_heads WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&expired); err != nil {
								return Record{}, err
							}
							if expired || (op.Cost != nil && op.Cost.State != "observed") {
								op.State = "incomplete"
								op.Reason = "operation_limit_or_cost_unconfirmed"
								return s.save(ctx, tx, w, p, r, "operation", op, deps)
							}
							nested, err := tx.Begin(ctx)
							if err != nil {
								return Record{}, err
							}
							if err = s.adopt(ctx, nested, p, w, old.Ref, &op); err != nil {
								_ = nested.Rollback(ctx)
								op.State = "proposal"
								op.Reason = "adoption_needs_review"
							} else if err = nested.Commit(ctx); err != nil {
								return Record{}, err
							}
						}
					}
				}
			}
		}
		return s.save(ctx, tx, w, p, r, "operation", op, deps)
	})
}
func unique(refs []Ref) []Ref {
	out := []Ref{}
	seen := map[Ref]bool{}
	for _, r := range refs {
		if !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	return out
}
func (s *Service) observe(ctx context.Context, tx pgx.Tx, p access.Principal, w string, operation Ref, op Operation, turn Turn, intent Record, reply Reply, unavailable, provisional bool) error {
	quantities := reply.Quantities()
	var actual *string
	if !unavailable && reply.Model != "" {
		actual = &reply.Model
	}
	coverage, state := "confirmed", "final"
	if reply.ID == "" || unavailable {
		coverage = "unknown"
		state = "unavailable"
	}
	if unavailable {
		for k := range quantities {
			quantities[k] = modelreply.Quantity(nil, nil)
		}
	}
	if provisional {
		state = "provisional"
	}
	o := usage.Observation{Source: "client-model:" + p.UserID + "/" + turn.Model.Connection, Transform: "client-chat-v1", Event: intent.ID, Series: intent.ID, Mode: "delta", Group: intent.ID, Request: reply.ID, Covers: []string{}, Coverage: coverage, Connection: turn.Model.Connection, Contract: turn.Model.Contract, RequestedModel: turn.Model.Requested, ActualModel: actual, At: time.Now().UTC(), Attribution: usage.Attribution{Kind: "operation", ID: operation.ID, Resource: operation.ID, Scopes: []string{}}, Quantities: quantities, Measurement: &usage.Measurement{State: state, Protocol: "openai-chat-completions", Scope: "client_operation_request", Generation: intent.ID, Billing: "unknown", SourceRef: intent.ID, SourceDigest: hash(turn.Response)}}
	deps := []usage.Ref{}
	for _, r := range append(append([]Ref{}, intent.Refs...), intent.Ref) {
		deps = append(deps, usage.Ref{ID: r.ID, Version: r.Version})
	}
	var expected string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')`, w, usage.ObservationID(o)).Scan(&expected); err != nil {
		return err
	}
	stage := "response"
	if provisional {
		stage = "intent"
	}
	observed, err := s.Usage.PutFromOwner(ctx, p, w, usage.ObservationID(o), usage.Write{Mutation: apicontract.Mutation{OperationID: "client-usage:" + intent.ID + ":" + stage, ExpectedVersion: expected}, Value: raw(o)}, usage.OwnerWrite{Actor: "client:" + p.UserID, Dependencies: deps, Check: func(ctx context.Context, tx pgx.Tx) error {
		return s.Access.CheckTx(ctx, tx, p, w, operation.ID, "update")
	}}, tx)
	if err != nil {
		return err
	}
	if observed.Error != nil || observed.Data == nil {
		return ErrState
	}
	if actual != nil {
		if price, ok := turn.Model.Prices[*actual]; ok {
			kind := turn.Model.Valuation
			if kind == "" {
				kind = "estimated"
			}
			e := usage.Evaluation{Group: o.Group, Kind: kind, Usage: []usage.Ref{{ID: observed.Data.ID, Version: observed.Data.Version}}, Price: &price}
			nested, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			_, err = accepted(s.Usage.Put(ctx, p, w, "evaluation:"+hash([]string{intent.ID, kind}), usage.Write{Mutation: apicontract.Mutation{OperationID: "client-evaluation:" + intent.ID, ExpectedVersion: "0"}, Value: raw(e)}, nested))
			if err != nil {
				_ = nested.Rollback(ctx)
				if errors.Is(err, usage.ErrInvalid) || errors.Is(err, access.ErrForbidden) || errors.Is(err, ErrState) {
					return nil
				}
				return err
			}
			return nested.Commit(ctx)
		}
	}
	return nil
}
