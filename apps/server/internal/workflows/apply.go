package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type ImpactRequest struct {
	Execution Ref `json:"execution"`
	Adoption  Ref `json:"adoption"`
}
type Impact struct {
	ImpactRequest
	Affected    []string `json:"affected_unstarted"`
	Invalidated []string `json:"invalidated_decisions"`
}
type Apply struct {
	apicontract.Mutation
	Impact Impact `json:"impact"`
}

func impact(old, next Definition, ts map[string]Task) (affected, invalidated []string) {
	affected = []string{}
	invalidated = []string{}
	before := map[string]Node{}
	after := map[string]Node{}
	for _, n := range old.Nodes {
		before[n.ID] = n
	}
	for _, n := range next.Nodes {
		after[n.ID] = n
	}
	changed := map[string]bool{}
	for id, n := range before {
		if !bytes.Equal(raw(n), raw(after[id])) {
			changed[id] = true
		}
	}
	for id, n := range after {
		if !bytes.Equal(raw(n), raw(before[id])) {
			changed[id] = true
		}
	}
	baseline := !bytes.Equal(raw(old.Baseline), raw(next.Baseline))
	for id := range changed {
		for descendant := range descendants(next, id) {
			changed[descendant] = true
		}
	}
	for id, t := range ts {
		if (changed[id] || baseline) && (t.Run == nil || t.Unstarted) {
			affected = append(affected, id)
			if t.DecisionAt != nil {
				invalidated = append(invalidated, id)
			}
		}
	}
	for id := range after {
		if _, exists := ts[id]; !exists {
			affected = append(affected, id)
		}
	}
	sort.Strings(affected)
	sort.Strings(invalidated)
	return
}
func (s *Service) Impact(ctx context.Context, p access.Principal, w string, req ImpactRequest) (Impact, error) {
	r, err := s.Read(ctx, p, w, req.Execution)
	if err != nil {
		return Impact{}, err
	}
	var e Execution
	if r.Kind != "execution" || json.Unmarshal(r.Value, &e) != nil {
		return Impact{}, ErrInvalid
	}
	current, err := record(ctx, s.Store.Database(), w, Ref{ID: r.ID, Version: "head"})
	if err != nil || current.Version != r.Version {
		return Impact{}, ErrState
	}
	old, _, err := s.definition(ctx, s.Store.Database(), p, w, e.Definition)
	if err != nil {
		return Impact{}, err
	}
	next, _, err := s.adopted(ctx, s.Store.Database(), p, w, req.Adoption)
	if err != nil {
		return Impact{}, err
	}
	ts, err := tasks(ctx, s.Store.Database(), w, r.ID)
	if err != nil {
		return Impact{}, err
	}
	if err = unstarted(ctx, s.Store.Database(), w, ts); err != nil {
		return Impact{}, err
	}
	affected, invalidated := impact(old, next, ts)
	return Impact{req, affected, invalidated}, nil
}
func (s *Service) Apply(ctx context.Context, p access.Principal, w, id string, req Apply) (apicontract.Response[apicontract.Reference], error) {
	if req.Impact.Execution != (Ref{ID: id, Version: req.ExpectedVersion}) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "start", "apply", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		e, old, err := execution(ctx, tx, w, id)
		if err != nil {
			return Record{}, err
		}
		if e.State != "running" && e.State != "waiting" {
			return Record{}, ErrState
		}
		if err = s.Access.CheckTx(ctx, tx, p, w, e.Definition.ID, "update"); err != nil {
			return Record{}, err
		}
		if err = s.Access.CheckTx(ctx, tx, access.Principal{UserID: e.Controller}, w, id, "execute"); err != nil {
			return Record{}, err
		}
		before, _, err := s.definition(ctx, tx, p, w, e.Definition)
		if err != nil {
			return Record{}, err
		}
		next, refs, err := s.adopted(ctx, tx, p, w, req.Impact.Adoption)
		if err != nil {
			return Record{}, err
		}
		ts, err := tasks(ctx, tx, w, id)
		if err != nil {
			return Record{}, err
		}
		if err = unstarted(ctx, tx, w, ts); err != nil {
			return Record{}, err
		}
		affected, invalidated := impact(before, next, ts)
		if !bytes.Equal(raw(affected), raw(req.Impact.Affected)) || !bytes.Equal(raw(invalidated), raw(req.Impact.Invalidated)) {
			return Record{}, ErrState
		}
		nodes := map[string]Node{}
		for _, n := range next.Nodes {
			nodes[n.ID] = n
		}
		for _, id := range affected {
			t := ts[id]
			node, exists := nodes[id]
			attempt := t.Attempt + 1
			if _, err = tx.Exec(ctx, `UPDATE workflow_tasks SET current=false WHERE workspace_id=$1 AND execution_id=$2 AND node_id=$3`, w, ref.ID, id); err != nil {
				return Record{}, err
			}
			t = Task{Node: id, Attempt: attempt, Definition: node.Definition, State: "pending", Repository: t.Repository, Previous: t.Run}
			if !exists {
				t.State = "skipped"
			}
			if err = s.saveTask(ctx, tx, w, ref, t, refs); err != nil {
				return Record{}, err
			}
		}
		var a Adoption
		ar, err := record(ctx, tx, w, req.Impact.Adoption)
		if err != nil {
			return Record{}, err
		}
		_ = json.Unmarshal(ar.Value, &a)
		e.Previous = &old.Ref
		e.Definition = a.Definition
		e.Baseline = next.Baseline
		e.State = "running"
		e.Reason = ""
		if next.Budget.Seconds > 0 {
			deadline := e.StartedAt.Add(time.Duration(next.Budget.Seconds) * time.Second)
			e.Deadline = &deadline
		} else {
			e.Deadline = nil
		}
		if _, err = s.save(ctx, tx, w, Ref{ID: "workflow-event:" + ref.Version, Version: ref.Version}, "event", req.Impact, append(refs, old.Ref), ref.ID); err != nil {
			return Record{}, err
		}
		return s.saveExecution(ctx, tx, w, ref, e, unique(append(old.Refs, refs...)))
	})
}

func unstarted(ctx context.Context, q query, w string, ts map[string]Task) error {
	for id, t := range ts {
		if t.Run == nil {
			continue
		}
		var started bool
		err := q.QueryRow(ctx, `SELECT r.payload->>'started_at' IS NOT NULL FROM run_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.id AND v.version=r.version WHERE r.workspace_id=$1 AND r.id=$2`, w, t.Run.ID).Scan(&started)
		if notFound(err) {
			started = false
		} else if err != nil {
			return err
		}
		t.Unstarted = !started
		ts[id] = t
	}
	return nil
}
