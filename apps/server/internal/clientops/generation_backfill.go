package clientops

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type GenerationBackfill struct {
	apicontract.Mutation
	Activation Ref       `json:"activation"`
	Client     string    `json:"client"`
	From       time.Time `json:"from"`
	Until      time.Time `json:"until"`
}

func (s *Service) BackfillGeneration(ctx context.Context, p access.Principal, w, id string, req GenerationBackfill) (apicontract.Response[apicontract.Reference], error) {
	if !strings.HasPrefix(id, "generation-backfill:") || req.ExpectedVersion != "0" || !pinned(req.Activation) || req.Client == "" || req.From.IsZero() || !req.Until.After(req.From) || p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		if err := s.generationControl(ctx, tx, p, w, &req.Activation); err != nil {
			return Record{}, err
		}
		r, err := record(ctx, tx, w, req.Activation)
		if err != nil {
			return Record{}, err
		}
		var activation GenerationActivation
		json.Unmarshal(r.Value, &activation)
		if activation.Client != req.Client {
			return Record{}, ErrState
		}
		var through int64
		var upper string
		if err = tx.QueryRow(ctx, `SELECT sequence FROM shared_streams WHERE workspace_id=$1`, w).Scan(&through); err != nil {
			return Record{}, err
		}
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(resource_id),'') FROM shared_versions WHERE workspace_id=$1 AND resource_id LIKE 'run:%'`, w).Scan(&upper); err != nil {
			return Record{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO context_generation_backfills(workspace_id,id,activation_id,activation_version,owner_user,client_id,from_at,until_at,through,upper_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, w, id, req.Activation.ID, req.Activation.Version, p.UserID, req.Client, req.From, req.Until, through, upper); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, p, ref, "generation-backfill", map[string]any{"activation": req.Activation, "from": req.From, "until": req.Until, "through": through, "state": "waiting"}, []Ref{req.Activation})
	})
}
func (s *Service) stepGenerationBackfill(ctx context.Context, tx pgx.Tx, p access.Principal, w, client string) error {
	var id, after, upper string
	var activationRef Ref
	var from, until time.Time
	var through int64
	err := tx.QueryRow(ctx, `SELECT id,activation_id,activation_version,from_at,until_at,after_id,upper_id,through FROM context_generation_backfills WHERE workspace_id=$1 AND owner_user=$2 AND client_id=$3 AND state='waiting' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`, w, p.UserID, client).Scan(&id, &activationRef.ID, &activationRef.Version, &from, &until, &after, &upper, &through)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	update := func(state string) error {
		_, err := tx.Exec(ctx, `UPDATE context_generation_backfills SET state=$3,after_id=$4 WHERE workspace_id=$1 AND id=$2`, w, id, state, after)
		return err
	}
	if s.generationControl(ctx, tx, p, w, &activationRef) != nil {
		return update("attention")
	}
	r, err := record(ctx, tx, w, activationRef)
	if err != nil {
		return err
	}
	var activation GenerationActivation
	json.Unmarshal(r.Value, &activation)
	ruleRecord, err := record(ctx, tx, w, activation.Rule)
	if err != nil {
		return err
	}
	var rule GenerationRule
	json.Unmarshal(ruleRecord.Value, &rule)
	// One keyset page of Run identities, followed by an indexed fixed snapshot
	// lookup per identity. Context bodies and all-Workspace ID arrays are absent.
	rows, err := tx.Query(ctx, `WITH ids AS (SELECT resource_id FROM shared_versions WHERE workspace_id=$1 AND resource_id LIKE 'run:%' AND resource_id>$2 AND resource_id<=$3 ORDER BY resource_id LIMIT 50) SELECT i.resource_id,COALESCE(r.version,''),COALESCE(r.position,0),COALESCE(r.payload->>'state',''),COALESCE(r.payload->>'finished_at',''),COALESCE(r.payload->'input','{}'::jsonb) FROM ids i LEFT JOIN LATERAL(SELECT version,position,payload FROM run_records WHERE workspace_id=$1 AND id=i.resource_id AND kind='run' AND position<=$4 ORDER BY position DESC LIMIT 1)r ON true ORDER BY i.resource_id`, w, after, upper, through)
	if err != nil {
		return err
	}
	type snapshot struct {
		Ref
		Position        int64
		State, Finished string
		Input           []byte
	}
	snapshots := []snapshot{}
	for rows.Next() {
		var row snapshot
		if err = rows.Scan(&row.ID, &row.Version, &row.Position, &row.State, &row.Finished, &row.Input); err != nil {
			rows.Close()
			return err
		}
		snapshots = append(snapshots, row)
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	for _, row := range snapshots {
		after = row.ID
		if row.Version == "" || (row.State != "succeeded" && row.State != "failed" && row.State != "cancelled") {
			continue
		}
		finished, err := time.Parse(time.RFC3339Nano, row.Finished)
		if err != nil || finished.Before(from) || finished.After(until) {
			continue
		}
		var input Ref
		json.Unmarshal(row.Input, &input)
		var b []byte
		if err = tx.QueryRow(ctx, `SELECT refs FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='input'`, w, input.ID, input.Version).Scan(&b); err != nil {
			return err
		}
		var deps []Ref
		json.Unmarshal(b, &deps)
		matched := false
		for _, target := range rule.Targets {
			matched = matched || target == "*" || target == row.ID
			for _, dep := range deps {
				matched = matched || target == dep.ID
			}
		}
		if !matched || s.checkRefs(ctx, tx, p, w, append(deps, row.Ref)) != nil {
			continue
		}
		key := hash([]Ref{activation.Rule, row.Ref})
		if _, err = tx.Exec(ctx, `INSERT INTO context_generation_queue(workspace_id,id,activation_id,activation_version,run_id,run_version,through,owner_user,client_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, w, key, activationRef.ID, activationRef.Version, row.ID, row.Version, row.Position, p.UserID, client); err != nil {
			return err
		}
	}
	if len(snapshots) < 50 {
		return update("complete")
	}
	return update("waiting")
}
