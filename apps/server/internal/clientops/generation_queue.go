package clientops

import (
	"context"
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type generationQueue struct {
	ID         string
	Activation Ref
	Run        Ref
	Through    int64
	After      int64
	Operation  string
}

// NextGeneration fulfills an already-enabled intent. It never contacts a model.
// Stable rule/output IDs and the shared receipts make a lost claim response safe.
func (s *Service) NextGeneration(ctx context.Context, p access.Principal, w, client string) (*Ref, error) {
	if client == "" || p.OperationID != "" {
		return nil, ErrInvalid
	}
	if err := s.Access.Check(ctx, p, w, "", "register"); err != nil {
		return nil, err
	}
	var result *Ref
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := s.stepGenerationBackfill(ctx, tx, p, w, client); err != nil {
			return err
		}
		var q generationQueue
		err := tx.QueryRow(ctx, `SELECT id,activation_id,activation_version,run_id,run_version,through,after_position,operation_id FROM context_generation_queue WHERE workspace_id=$1 AND owner_user=$2 AND client_id=$3 AND state IN ('waiting','active') ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, w, p.UserID, client).Scan(&q.ID, &q.Activation.ID, &q.Activation.Version, &q.Run.ID, &q.Run.Version, &q.Through, &q.After, &q.Operation)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		update := func(state string) error {
			_, err := tx.Exec(ctx, `UPDATE context_generation_queue SET state=$3,after_position=$4,operation_id=$5 WHERE workspace_id=$1 AND id=$2`, w, q.ID, state, q.After, q.Operation)
			return err
		}
		if s.generationControl(ctx, tx, p, w, &q.Activation) != nil {
			return update("attention")
		}
		if q.Operation != "" {
			record, op, err := s.owner(ctx, tx, p, w, q.Operation)
			if err != nil {
				return err
			}
			if op.State == "ready" {
				ref := record.Ref
				result = &ref
				return nil
			}
			if op.State != "applied" && op.State != "cancelled" {
				ref := record.Ref
				result = &ref
				return update("attention")
			}
			q.Operation = ""
		}
		activationRecord, err := record(ctx, tx, w, q.Activation)
		if err != nil {
			return err
		}
		var activation GenerationActivation
		json.Unmarshal(activationRecord.Value, &activation)
		ruleRecord, err := record(ctx, tx, w, activation.Rule)
		if err != nil {
			return err
		}
		var rule GenerationRule
		if decode(ruleRecord.Value, &rule) != nil {
			return ErrState
		}
		// Each page is bound to one completed Run and one explicitly selected channel.
		// Advancing a cursor does not enumerate Workspace Contexts or transcripts.
		rows, err := tx.Query(ctx, `SELECT id,version,position FROM run_records WHERE workspace_id=$1 AND run_id=$2 AND kind='output' AND payload->>'channel'=$3 AND position>$4 AND position<=$5 ORDER BY position,id LIMIT 50`, w, q.Run.ID, rule.Channel, q.After, q.Through)
		if err != nil {
			return err
		}
		type sourceOutput struct {
			Ref
			Position int64
		}
		outputs := []sourceOutput{}
		for rows.Next() {
			var out sourceOutput
			if err = rows.Scan(&out.ID, &out.Version, &out.Position); err != nil {
				rows.Close()
				return err
			}
			outputs = append(outputs, out)
		}
		rows.Close()
		if rows.Err() != nil {
			return rows.Err()
		}
		if len(outputs) == 0 {
			return update("complete")
		}
		for _, output := range outputs {
			q.After = output.Position
			key := hash([]Ref{activation.Rule, output.Ref})
			id := "client-operation:context-auto:" + key
			existing, readErr := record(ctx, tx, w, Ref{ID: id, Version: "head"})
			if readErr == nil {
				var op Operation
				if decode(existing.Value, &op) != nil {
					return ErrState
				}
				if op.State == "applied" || op.State == "cancelled" {
					continue
				}
				q.Operation = id
				ref := existing.Ref
				result = &ref
				if op.State == "ready" {
					return update("active")
				}
				return update("attention")
			}
			if readErr != pgx.ErrNoRows {
				return readErr
			}
			var policy Policy
			json.Unmarshal(raw(rule.Template), &policy)
			policy.Generation.Source.Run = q.Run
			policy.Generation.Source.Output = output.Ref
			policy.Generation.Activation = &q.Activation
			policyRef, err := accepted(s.PutPolicy(ctx, p, w, "context-policy:auto:"+key, Write{Mutation: apicontract.Mutation{OperationID: "generation-policy:" + key, ExpectedVersion: "0"}, Value: raw(policy)}, tx))
			if err != nil {
				return err
			}
			opRef, err := accepted(s.Start(ctx, p, w, id, Start{Mutation: apicontract.Mutation{OperationID: "generation-start:" + key, ExpectedVersion: "0"}, Policy: policyRef, Target: Ref{ID: "context-result:auto:" + key, Version: "0"}, Purpose: "context-generation", Goal: rule.Goal, Client: client, Model: activation.Model}, tx))
			if err != nil {
				return err
			}
			q.Operation = id
			result = &opRef
			return update("active")
		}
		return update("waiting")
	})
	return result, err
}
func (s *Service) GenerationQueue(ctx context.Context, p access.Principal, w, after string) (Page, error) {
	page := Page{Items: []Record{}}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return page, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT id,activation_id,activation_version,run_id,run_version,state,operation_id,client_id FROM context_generation_queue WHERE workspace_id=$1 AND owner_user=$2 AND id>$3 ORDER BY id LIMIT 50`, w, p.UserID, after)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, state, operation, client string
		var activation, run Ref
		if err = rows.Scan(&id, &activation.ID, &activation.Version, &run.ID, &run.Version, &state, &operation, &client); err != nil {
			return page, err
		}
		page.Items = append(page.Items, Record{Ref: Ref{ID: id, Version: activation.Version}, Kind: "generation-queue", Value: raw(map[string]any{"activation": activation, "run": run, "state": state, "operation": operation, "client": client})})
	}
	if len(page.Items) > 0 {
		page.AfterID = page.Items[len(page.Items)-1].ID
		page.After = int64(len(page.Items))
	}
	return page, rows.Err()
}
