package automation

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

type triageWait string

func (e triageWait) Error() string { return string(e) }
func (e triageWait) Unwrap() error { return access.ErrForbidden }

type BatchEdit struct {
	Policy   Ref               `json:"policy"`
	Scopes   []string          `json:"scopes"`
	From     time.Time         `json:"from"`
	Until    time.Time         `json:"until"`
	Count    int               `json:"count"`
	PageSize int               `json:"page_size"`
	Cost     *usage.CostBudget `json:"cost,omitempty"`
	Continue bool              `json:"continue"`
	Stop     bool              `json:"stop"`
}
type Batch struct {
	Policy    Ref               `json:"policy"`
	Scopes    []string          `json:"scopes"`
	From      time.Time         `json:"from"`
	Until     time.Time         `json:"until"`
	Count     int               `json:"count"`
	PageSize  int               `json:"page_size"`
	Cost      *usage.CostBudget `json:"cost,omitempty"`
	At        time.Time         `json:"reference_time"`
	Position  int64             `json:"reference_position"`
	After     string            `json:"after"`
	Requested int               `json:"requested"`
	Scanned   int               `json:"scanned"`
	State     string            `json:"state"`
	Reason    string            `json:"reason"`
}

func (s *Service) batchReadable(ctx context.Context, q query, p access.Principal, w, id string) error {
	var denied bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_batch_jobs b JOIN automation_jobs j ON j.workspace_id=b.workspace_id AND j.id=b.job_id WHERE b.workspace_id=$1 AND b.batch_id=$4 AND NOT (`+access.ReadFilterSQL("j.refs||COALESCE(j.result->'refs','[]'::jsonb)||COALESCE(j.result->'usage','[]'::jsonb)")+`))`, w, p.UserID, p.OperationID, id).Scan(&denied)
	if err == nil && denied {
		return access.ErrForbidden
	}
	return err
}

func (s *Service) prepareBatch(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string, raw []byte) (any, []Ref, error) {
	if err := s.manage(ctx, tx, p, w); err != nil {
		return nil, nil, err
	}
	var edit BatchEdit
	if decode(raw, &edit) != nil {
		return nil, nil, ErrInvalid
	}
	var batch Batch
	previous, err := record(ctx, tx, w, Ref{ID: id, Version: "head"})
	if err == nil {
		if json.Unmarshal(previous.Value, &batch) != nil {
			return nil, nil, ErrInvalid
		}
		if err = s.batchReadable(ctx, tx, p, w, id); err != nil {
			return nil, nil, err
		}
		if !edit.Continue && !edit.Stop {
			return nil, nil, ErrInvalid
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		if edit.Continue || edit.Stop || !fixed(edit.Policy) || len(edit.Scopes) == 0 || edit.From.IsZero() || edit.Until.IsZero() || !edit.From.Before(edit.Until) || edit.Count < 1 || edit.PageSize < 1 || edit.PageSize > 100 || (edit.Cost != nil && !usage.ValidCostBudget(*edit.Cost)) {
			return nil, nil, ErrInvalid
		}
		batch = Batch{Policy: edit.Policy, Scopes: edit.Scopes, From: edit.From, Until: edit.Until, Count: edit.Count, PageSize: edit.PageSize, Cost: edit.Cost, At: time.Now().UTC(), State: "ready"}
		if err = tx.QueryRow(ctx, `SELECT sequence FROM shared_streams WHERE workspace_id=$1`, w).Scan(&batch.Position); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}
	refs := []Ref{batch.Policy}
	for _, id := range batch.Scopes {
		if !strings.HasPrefix(id, "scope:") {
			return nil, nil, ErrInvalid
		}
		refs = append(refs, Ref{ID: id})
	}
	if edit.Stop {
		batch.State = "stopped"
		return batch, refs, nil
	}
	if batch.State == "complete" {
		return batch, refs, nil
	}
	if batch.Cost != nil {
		state, err := s.batchCost(ctx, tx, p, w, id, *batch.Cost)
		if err != nil {
			return nil, nil, err
		}
		if state != "observed" {
			batch.State = "held"
			batch.Reason = "cost_" + state
			return batch, refs, nil
		}
	}
	remaining := batch.Count - batch.Requested
	if remaining <= 0 {
		batch.State = "complete"
		return batch, refs, nil
	}
	limit := min(batch.PageSize, remaining)
	// Scope ancestry is read as of the saved stream position. Only Scope metadata
	// is traversed (at most the existing four levels); no Context body is loaded.
	rows, err := tx.Query(ctx, `WITH RECURSIVE groups(id,depth) AS (
 SELECT unnest($2::text[]),0 UNION ALL
 SELECT c.resource_id,g.depth+1 FROM groups g JOIN context_records c ON c.workspace_id=$1 AND c.kind='scope' AND c.payload#>>'{parent,id}'=substring(g.id from 7) AND c.stream_sequence<=$3 AND c.payload->>'kind'<>'issue'
 WHERE g.depth<3 AND NOT EXISTS(SELECT 1 FROM context_records n WHERE n.workspace_id=c.workspace_id AND n.resource_id=c.resource_id AND n.stream_sequence>c.stream_sequence AND n.stream_sequence<=$3))
 SELECT t.scope_id,t.scope_version FROM triage_targets t WHERE t.workspace_id=$1 AND t.scope_id>$4 AND t.received_at>=$5 AND t.received_at<=$6 AND EXISTS(
 SELECT 1 FROM context_records c WHERE c.workspace_id=t.workspace_id AND c.resource_id=t.scope_id AND c.kind='scope' AND c.stream_sequence<=$3
 AND NOT EXISTS(SELECT 1 FROM context_records n WHERE n.workspace_id=c.workspace_id AND n.resource_id=c.resource_id AND n.stream_sequence>c.stream_sequence AND n.stream_sequence<=$3)
 AND (c.resource_id=ANY($2) OR ('scope:'||(c.payload#>>'{parent,id}')) IN (SELECT id FROM groups))) ORDER BY t.scope_id LIMIT $7`, w, batch.Scopes, batch.Position, batch.After, batch.From, batch.Until, limit+1)
	if err != nil {
		return nil, nil, err
	}
	targets := []Ref{}
	for rows.Next() {
		var target Ref
		if err = rows.Scan(&target.ID, &target.Version); err != nil {
			rows.Close()
			return nil, nil, err
		}
		targets = append(targets, target)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	more := len(targets) > limit
	if more {
		targets = targets[:limit]
	}
	for _, target := range targets {
		batch.After = target.ID
		batch.Scanned++
		selected, reason, deps, err := s.selectPolicy(ctx, tx, w, target)
		if err != nil {
			return nil, nil, err
		}
		if reason != "" || selected.Ref != batch.Policy {
			continue
		}
		if s.checkRefs(ctx, tx, p, w, deps) != nil {
			continue
		}
		if err = s.queueTriage(ctx, tx, w, target, "batch:"+id+":"+target.ID); err != nil {
			return nil, nil, err
		}
		var job string
		if err = tx.QueryRow(ctx, `SELECT latest_job FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2`, w, target.ID).Scan(&job); err != nil {
			return nil, nil, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO triage_batch_jobs VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, w, id, job); err != nil {
			return nil, nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET checkpoint=jsonb_build_object('reference_time',$3::text) WHERE workspace_id=$1 AND id=$2`, w, job, batch.At.Format(time.RFC3339Nano)); err != nil {
			return nil, nil, err
		}
		batch.Requested++
	}
	batch.State = "ready"
	batch.Reason = ""
	if !more || batch.Requested >= batch.Count {
		batch.State = "complete"
	}
	return batch, refs, nil
}

func (s *Service) batchCost(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string, b usage.CostBudget) (string, error) {
	if err := s.batchReadable(ctx, tx, p, w, id); err != nil {
		return "", err
	}
	var missing bool
	var amount string
	err := tx.QueryRow(ctx, `WITH groups AS (
 SELECT DISTINCT a.resource_id FROM triage_batch_jobs b JOIN triage_model_attempts a ON a.workspace_id=b.workspace_id AND a.job_id=b.job_id WHERE b.workspace_id=$1 AND b.batch_id=$2),basis AS (
 SELECT e.amount,e.currency,e.state,EXISTS(SELECT 1 FROM usage_group_updates u WHERE u.workspace_id=$1 AND u.coverage_group=g.resource_id AND u.position>e.position) AS stale
 FROM groups g LEFT JOIN LATERAL (SELECT DISTINCT ON(projection_key) * FROM usage_entries e WHERE e.workspace_id=$1 AND e.coverage_group=g.resource_id AND e.kind=$3 ORDER BY projection_key,position DESC) e ON true)
 SELECT EXISTS(SELECT 1 FROM basis WHERE amount IS NULL OR currency<>$4 OR state<>'rated' OR stale),COALESCE((SELECT sum(amount)::text FROM basis),'0')`, w, id, b.Valuation, b.Currency).Scan(&missing, &amount)
	if err != nil {
		return "", err
	}
	if missing {
		return "unobserved", nil
	}
	total, ok := new(big.Rat).SetString(amount)
	if !ok {
		return "unobserved", nil
	}
	limit, _ := new(big.Rat).SetString(b.Amount)
	if total.Cmp(limit) >= 0 {
		return "exhausted", nil
	}
	return "observed", nil
}

func (s *Service) checkBatch(ctx context.Context, tx pgx.Tx, j Job, p access.Principal, spending bool) error {
	rows, err := tx.Query(ctx, `SELECT b.batch_id,r.payload FROM triage_batch_jobs b JOIN shared_versions h ON h.workspace_id=b.workspace_id AND h.resource_id=b.batch_id JOIN automation_records r ON r.workspace_id=h.workspace_id AND r.id=h.resource_id AND r.version=h.version WHERE b.workspace_id=$1 AND b.job_id=$2`, j.Workspace, j.ID)
	if err != nil {
		return err
	}
	type entry struct {
		id  string
		raw []byte
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.raw); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		var b Batch
		if json.Unmarshal(e.raw, &b) != nil {
			return ErrInvalid
		}
		if b.State == "stopped" {
			return access.ErrForbidden
		}
		if spending && b.Cost != nil {
			state, err := s.batchCost(ctx, tx, p, j.Workspace, e.id, *b.Cost)
			if err != nil {
				return err
			}
			if state != "observed" {
				return triageWait("batch_cost_" + state)
			}
		}
	}
	return nil
}
