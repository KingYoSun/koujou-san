package workflows

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

func (s *Service) StartWorker(ctx context.Context) {
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				rows, err := s.Store.Database().Query(ctx, `SELECT workspace_id,execution_id FROM workflow_ready WHERE state IN ('running','waiting') AND due_at<=now() ORDER BY due_at,workspace_id,execution_id LIMIT 50`)
				if err != nil {
					continue
				}
				type key struct{ w, id string }
				keys := []key{}
				for rows.Next() {
					var k key
					if rows.Scan(&k.w, &k.id) == nil {
						keys = append(keys, k)
					}
				}
				rows.Close()
				for _, k := range keys {
					_ = s.Step(ctx, k.w, k.id)
				}
			}
		}
	}()
}
func ciVerdict(command CICommand, snap githubobserve.Snapshot) string {
	if snap.Commit != command.Commit || strconv.FormatInt(snap.RunID, 10) != command.RunID || snap.State != "completed" {
		return "INCONCLUSIVE"
	}
	for _, name := range command.RequiredJobs {
		found := false
		for _, j := range snap.Jobs {
			if j.Name == name && j.Attempt == snap.Attempt {
				found = true
				if j.Status != "completed" || j.Conclusion == "skipped" || j.Conclusion == "neutral" || j.Conclusion == "" {
					return "INCONCLUSIVE"
				}
				if j.Conclusion != "success" {
					return "FAIL"
				}
			}
		}
		if !found {
			return "INCONCLUSIVE"
		}
	}
	if snap.Conclusion != "success" {
		return "FAIL"
	}
	return "PASS"
}

// Step owns a session advisory lock across the separate durable intent, Run
// create and assign transactions. No Workspace lock is held during a remote read.
func (s *Service) Step(ctx context.Context, w, id string) error {
	conn, err := s.Store.Database().Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,20020))`, w+":"+id).Scan(&locked)
	if err != nil || !locked {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,20020))`, w+":"+id)
	defer s.Store.Database().Exec(ctx, `UPDATE workflow_ready SET due_at=now()+interval '1 second' WHERE workspace_id=$1 AND execution_id=$2`, w, id)
	e, root, err := execution(ctx, s.Store.Database(), w, id)
	if err != nil {
		return err
	}
	if e.State != "running" && e.State != "waiting" {
		return nil
	}
	p := access.Principal{UserID: e.Controller}
	if err = s.Access.Check(ctx, p, w, id, "execute"); err != nil {
		return err
	}
	if err = s.Access.Check(ctx, p, w, e.Definition.ID, "execute"); err != nil {
		return err
	}
	d, _, err := s.definition(ctx, s.Store.Database(), p, w, e.Definition)
	if err != nil {
		return err
	}
	ts, err := tasks(ctx, s.Store.Database(), w, id)
	if err != nil {
		return err
	}
	// Reconcile only the current attempts, never historical Run/output streams.
	var preparedTask *Task
	for _, n := range d.Nodes {
		t := ts[n.ID]
		if t.Run == nil || (t.State != "running" && t.State != "prepared") {
			continue
		}
		rr, readErr := s.Runs.Read(ctx, p, w, Ref{ID: t.Run.ID, Version: "head"})
		if readErr == nil {
			var run runs.Run
			_ = json.Unmarshal(rr.Value, &run)
			if terminal(run.State) {
				t.State = "done"
				t.Run = &rr.Ref
				t.Outcome = run.State
				if run.Assessment != nil {
					if run.Assessment.State != "valid" || run.State != "succeeded" {
						t.Outcome = "INCONCLUSIVE"
					} else if run.Assessment.Verdict != "" {
						t.Outcome = run.Assessment.Verdict
					}
				}
				if t.Stale {
					t.Outcome = "STALE"
				}
				return s.transition(ctx, p, w, root, e, []Task{t}, "completed")
			}
		}
		if t.State == "prepared" {
			copy := t
			preparedTask = &copy
		}
	}
	if e.Deadline != nil && time.Now().After(*e.Deadline) {
		e.State = "stopped"
		e.Reason = "time_budget_no_further_start"
		return s.transition(ctx, p, w, root, e, nil, "budget")
	}
	if d.Budget.Cost != nil {
		cost, err := s.cost(ctx, s.Store.Database(), p, w, id, *d.Budget.Cost)
		if err != nil {
			return err
		}
		if cost.State != "observed" {
			if e.Cost != nil && string(raw(e.Cost)) == string(raw(cost)) {
				return nil
			}
			e.Cost = &cost
			e.State = "waiting"
			e.Reason = "cost_" + cost.State + "_no_further_start"
			return s.transition(ctx, p, w, root, e, nil, "cost")
		}
		e.Cost = &cost
	}
	if preparedTask != nil {
		return s.launch(ctx, p, w, root, e, *preparedTask)
	}
	for _, n := range d.Nodes {
		t := ts[n.ID]
		if t.State != "done" || t.Retried || n.Retry[t.Outcome] == "" {
			continue
		}
		if d.Budget.MaxRuns > 0 && e.Count >= d.Budget.MaxRuns {
			e.State = "stopped"
			e.Reason = "run_budget"
			return s.transition(ctx, p, w, root, e, nil, "budget")
		}
		affected := descendants(d, n.Retry[t.Outcome])
		changes := []Task{}
		for _, node := range d.Nodes {
			if !affected[node.ID] {
				continue
			}
			old := ts[node.ID]
			if old.State == "running" || old.State == "prepared" {
				return nil
			}
			next := Task{Node: node.ID, Attempt: old.Attempt + 1, Definition: node.Definition, State: "pending", Repository: old.Repository, Previous: old.Run}
			changes = append(changes, next)
		}
		t.Retried = true
		if !affected[t.Node] {
			changes = append(changes, t)
		}
		return s.transition(ctx, p, w, root, e, changes, "retry")
	}
	active, unfinished := 0, 0
	for _, t := range ts {
		if t.State == "running" || t.State == "prepared" {
			active++
		}
		if t.State != "done" && t.State != "skipped" {
			unfinished++
		}
	}
	if unfinished == 0 {
		e.State = "completed"
		e.Reason = ""
		return s.transition(ctx, p, w, root, e, nil, "completed")
	}
	for _, n := range d.Nodes {
		t := ts[n.ID]
		if t.State != "pending" {
			continue
		}
		ready, impossible := readiness(n, ts)
		if impossible {
			t.State = "skipped"
			return s.transition(ctx, p, w, root, e, []Task{t}, "branch_skipped")
		}
		if !ready {
			continue
		}
		if n.Definition == nil {
			t.State = "decision"
			e.State = "waiting"
			e.Reason = "human_decision"
			return s.transition(ctx, p, w, root, e, []Task{t}, "decision_wait")
		}
		if active >= d.Budget.Parallel {
			continue
		}
		if d.Budget.MaxRuns > 0 && e.Count >= d.Budget.MaxRuns {
			e.State = "stopped"
			e.Reason = "run_budget"
			return s.transition(ctx, p, w, root, e, nil, "budget")
		}
		run := newRun()
		prepared, err := s.prepare(ctx, p, w, run.ID, e, n, ts)
		if err != nil {
			return err
		}
		t.Run = &run
		t.Prepared = &prepared
		t.Inputs = prepared.Sources
		t.State = "prepared"
		e.Count++
		e.State = "running"
		e.Reason = ""
		return s.transition(ctx, p, w, root, e, []Task{t}, "prepared")
	}
	return nil
}
func descendants(d Definition, id string) map[string]bool {
	out := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, n := range d.Nodes {
			if out[n.ID] {
				continue
			}
			for _, dep := range n.Depends {
				if out[dep.Node] {
					out[n.ID] = true
					changed = true
					break
				}
			}
		}
	}
	return out
}
func (s *Service) transition(ctx context.Context, p access.Principal, w string, old Record, e Execution, changes []Task, event string) error {
	_, err := accepted(s.mutate(ctx, p, w, old.ID, "start", event, operation(old.ID, "step:"+old.Version, old.Version), struct {
		Execution Execution
		Tasks     []Task
		Event     string
	}{e, changes, event}, func(tx pgx.Tx, ref Ref) (Record, error) {
		if err := s.checkRefs(ctx, tx, p, w, old.Refs); err != nil {
			return Record{}, err
		}
		for _, t := range changes {
			if _, err := tx.Exec(ctx, `UPDATE workflow_tasks SET current=false WHERE workspace_id=$1 AND execution_id=$2 AND node_id=$3 AND attempt<>$4`, w, old.ID, t.Node, t.Attempt); err != nil {
				return Record{}, err
			}
			if t.Prepared != nil && t.State == "prepared" {
				fixedInput := false
				for _, source := range t.Prepared.Spec.Sources {
					if source.ID == taskID(old.ID, t) {
						fixedInput = true
					}
				}
				if !fixedInput {
					t.Prepared.Spec.Sources = unique(append(t.Prepared.Spec.Sources, ref, Ref{ID: taskID(old.ID, t), Version: ref.Version}))
				}
				t.Inputs = t.Prepared.Spec.Sources
			}
			if err := s.saveTask(ctx, tx, w, ref, t, old.Refs); err != nil {
				return Record{}, err
			}
		}
		changedRefs := []Ref{}
		for _, t := range changes {
			changedRefs = append(changedRefs, Ref{ID: taskID(old.ID, t), Version: ref.Version})
		}
		if _, err := s.save(ctx, tx, w, Ref{ID: "workflow-event:" + ref.Version, Version: ref.Version}, "event", map[string]any{"event": event, "previous": old.Ref, "definition": e.Definition, "tasks": changedRefs}, old.Refs, old.ID); err != nil {
			return Record{}, err
		}
		return s.saveExecution(ctx, tx, w, ref, e, old.Refs)
	}))
	return err
}
func (s *Service) launch(ctx context.Context, p access.Principal, w string, root Record, e Execution, t Task) error {
	if t.Run == nil || t.Prepared == nil || t.Definition == nil {
		return ErrState
	}
	if err := s.beforeStart(ctx, nil, w, t.Run.ID); err != nil {
		return err
	}
	d, _, err := s.definition(ctx, s.Store.Database(), p, w, *t.Definition)
	if err != nil {
		return err
	}
	var observation *runs.Observation
	if d.CI != nil {
		if s.Observer == nil {
			return ErrState
		}
		snap, err := s.Observer.Current(ctx, p, w, d.CI.Target)
		if err != nil {
			return err
		}
		observation = &runs.Observation{Verdict: ciVerdict(*d.CI, snap), Report: raw(map[string]any{"command_state": "completed", "source": "github-observation", "target": d.CI.Target, "observation": snap})}
	}
	spec := t.Prepared.Spec
	spec.Previous = t.Previous
	ref, err := accepted(s.Runs.CreateForWorkflow(ctx, p, w, t.Run.ID, runs.Create{Mutation: operation(t.Run.ID, "create", "0"), Spec: spec}, observation))
	if err != nil {
		return err
	}
	if d.CI == nil {
		rr, err := s.Runs.Read(ctx, p, w, Ref{ID: ref.ID, Version: "head"})
		if err != nil {
			return err
		}
		var run runs.Run
		_ = json.Unmarshal(rr.Value, &run)
		if run.Assignment == nil {
			if d.DeviceID == "" {
				return ErrState
			}
			profile, err := s.Capabilities(ctx, p, w, d.DeviceID, "head")
			if err != nil {
				return err
			}
			if t.AssignmentProfile != nil && *t.AssignmentProfile == profile.Ref && run.Preparation != "" {
				return nil
			}
			t.AssignmentProfile = &profile.Ref
			ref, err = accepted(s.Runs.Assign(ctx, p, w, ref.ID, runs.Assign{Mutation: operation(ref.ID, "assign:"+rr.Version, rr.Version), DeviceID: d.DeviceID, Required: d.Required, Repository: t.Repository}))
			if err != nil {
				return err
			}
			updated, err := s.Runs.Read(ctx, p, w, ref)
			if err != nil {
				return err
			}
			_ = json.Unmarshal(updated.Value, &run)
			if run.Assignment == nil {
				t.Run = &ref
				e.State = "waiting"
				e.Reason = "runner_preparation:" + run.Preparation
				return s.transition(ctx, p, w, root, e, []Task{t}, "runner_wait")
			}
		}
	}
	t.Run = &ref
	t.State = "running"
	e.State = "running"
	e.Reason = ""
	return s.transition(ctx, p, w, root, e, []Task{t}, "assigned")
}
