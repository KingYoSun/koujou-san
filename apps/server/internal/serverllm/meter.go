package serverllm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
	"time"
)

type MeterInput struct {
	ID      string
	Version string
	Refs    []Ref
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func Digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func acceptedUsage(v apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if v.Data == nil {
		return Ref{}, ErrUnavailable
	}
	return Ref{ID: v.Data.ID, Version: v.Data.Version}, nil
}

// Observe is shared by Hopper and Triage. The caller supplies a reservation
// guard; after dispatch, completion may preserve usage despite revoked authority.
func Observe(ctx context.Context, tx pgx.Tx, p access.Principal, w, owner, evaluation, operation string, intent Ref, input MeterInput, spec *Spec, reply modelreply.Reply, response *Response, provisional bool, service *usage.Service, guard usage.OwnerWrite) ([]Ref, []Ref, error) {
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
	observation := usage.Observation{Source: "server-model:" + spec.Connection, Transform: "server-chat-v1", Event: intent.ID, Series: intent.ID, Mode: "delta", Group: intent.ID, Request: request, Covers: []string{}, Coverage: coverage, Connection: spec.Connection, Contract: spec.Contract, RequestedModel: spec.Model, ActualModel: actual, At: time.Now().UTC(), Attribution: usage.Attribution{Kind: owner, ID: evaluation, Resource: input.ID, Scopes: []string{}}, Quantities: quantities, Measurement: &usage.Measurement{State: state, Protocol: "openai-chat-completions", Scope: owner + "_evaluation_request", Generation: intent.ID, Billing: "unknown", SourceRef: intent.ID, SourceDigest: Digest(response)}}
	id := usage.ObservationID(observation)
	var expected string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')`, w, id).Scan(&expected); err != nil {
		return nil, nil, err
	}
	observed, err := service.PutFromOwner(ctx, p, w, id, usage.Write{Mutation: apicontract.Mutation{OperationID: operation, ExpectedVersion: expected}, Value: raw(observation)}, guard, tx)
	ref, err := acceptedUsage(observed, err)
	if err != nil {
		return nil, nil, err
	}
	refs := []Ref{ref}
	costs := []Ref{}
	if actual != nil {
		if price, ok := spec.Prices[*actual]; ok {
			valuation := spec.Valuation
			if valuation == "" {
				valuation = "estimated"
			}
			nested, err := tx.Begin(ctx)
			if err != nil {
				return nil, nil, err
			}
			cost, err := acceptedUsage(service.Put(ctx, p, w, "evaluation:"+Digest([]string{intent.ID, valuation}), usage.Write{Mutation: apicontract.Mutation{OperationID: owner + "-price:" + intent.ID, ExpectedVersion: "0"}, Value: raw(usage.Evaluation{Group: observation.Group, Kind: valuation, Usage: []usage.Ref{{ID: ref.ID, Version: ref.Version}}, Price: &price})}, nested))
			if err != nil {
				_ = nested.Rollback(ctx)
				if !errors.Is(err, usage.ErrInvalid) && !errors.Is(err, access.ErrForbidden) && !errors.Is(err, ErrUnavailable) {
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
