package workflows

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

type Start struct {
	apicontract.Mutation
	Adoption     Ref                                   `json:"adoption"`
	Inputs       map[string]map[string]json.RawMessage `json:"inputs,omitempty"`
	InputsUTF8   *string                               `json:"inputs_utf8,omitempty"`
	Repositories map[string]runs.Repository            `json:"repositories,omitempty"`
}
type Decision struct {
	apicontract.Mutation
	Node    string `json:"node"`
	Attempt int    `json:"attempt"`
	Choice  string `json:"choice"`
}

func execution(ctx context.Context, q query, w, id string) (Execution, Record, error) {
	r, err := record(ctx, q, w, Ref{ID: id, Version: "head"})
	var e Execution
	if err == nil && (r.Kind != "execution" || json.Unmarshal(r.Value, &e) != nil) {
		err = ErrInvalid
	}
	return e, r, err
}
func tasks(ctx context.Context, q query, w, id string) (map[string]Task, error) {
	rows, err := q.Query(ctx, `SELECT payload FROM workflow_tasks WHERE workspace_id=$1 AND execution_id=$2 AND current ORDER BY node_id`, w, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Task{}
	for rows.Next() {
		var b []byte
		var t Task
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &t); err != nil {
			return nil, err
		}
		out[t.Node] = t
	}
	return out, rows.Err()
}
func taskID(id string, t Task) string {
	return fmt.Sprintf("workflow-task:%s:%s:%d", strings.TrimPrefix(id, "workflow-execution:"), t.Node, t.Attempt)
}
func (s *Service) saveTask(ctx context.Context, tx pgx.Tx, w string, root Ref, t Task, refs []Ref) error {
	run := ""
	if t.Run != nil {
		run = t.Run.ID
	}
	_, err := tx.Exec(ctx, `INSERT INTO workflow_tasks VALUES($1,$2,$3,$4,$5,true,$6,$7) ON CONFLICT(workspace_id,execution_id,node_id,attempt) DO UPDATE SET state=excluded.state,payload=excluded.payload,run_id=excluded.run_id`, w, root.ID, t.Node, t.Attempt, run, t.State, raw(t))
	if err != nil {
		return err
	}
	_, err = s.save(ctx, tx, w, Ref{ID: taskID(root.ID, t), Version: root.Version}, "task", t, append(refs, t.Inputs...), root.ID)
	return err
}
func (s *Service) saveExecution(ctx context.Context, tx pgx.Tx, w string, ref Ref, e Execution, refs []Ref) (Record, error) {
	if e.Cost != nil {
		refs = append(refs, e.Cost.Sources...)
	}
	_, err := tx.Exec(ctx, `INSERT INTO workflow_ready(workspace_id,execution_id,state) VALUES($1,$2,$3) ON CONFLICT(workspace_id,execution_id) DO UPDATE SET state=excluded.state,due_at=now()`, w, ref.ID, e.State)
	if err != nil {
		return Record{}, err
	}
	return s.save(ctx, tx, w, ref, "execution", e, refs)
}
func (s *Service) adopted(ctx context.Context, q query, p access.Principal, w string, ref Ref) (Definition, []Ref, error) {
	r, err := record(ctx, q, w, ref)
	if err != nil {
		return Definition{}, nil, err
	}
	var a Adoption
	if r.Kind != "adoption" || !fixed(ref) || json.Unmarshal(r.Value, &a) != nil {
		return Definition{}, nil, ErrInvalid
	}
	v, err := s.validation(ctx, q, p, w, a.Definition)
	if err != nil {
		return Definition{}, nil, err
	}
	if !v.Valid {
		return Definition{}, nil, ErrState
	}
	d, _, err := s.definition(ctx, q, p, w, a.Definition)
	if err != nil {
		return d, nil, err
	}
	if d.Schema != "workflow-definition-v1" {
		return d, nil, ErrInvalid
	}
	return d, unique(append(append(v.Checked, r.Refs...), r.Ref)), s.checkRefs(ctx, q, p, w, append(r.Refs, r.Ref))
}
func (s *Service) Start(ctx context.Context, p access.Principal, w, id string, req Start) (apicontract.Response[apicontract.Reference], error) {
	if req.InputsUTF8 != nil {
		if req.Inputs != nil || decode([]byte(*req.InputsUTF8), &req.Inputs) != nil {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
	}
	if kind(id) != "workflow-execution" || req.ExpectedVersion != "0" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, "start", "start", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		d, refs, err := s.adopted(ctx, tx, p, w, req.Adoption)
		if err != nil {
			return Record{}, err
		}
		var pin Adoption
		adoptedRecord, err := record(ctx, tx, w, req.Adoption)
		if err != nil {
			return Record{}, err
		}
		_ = json.Unmarshal(adoptedRecord.Value, &pin)
		if err = s.Access.CheckTx(ctx, tx, p, w, pin.Definition.ID, "execute"); err != nil {
			return Record{}, err
		}
		var a Adoption
		ad, err := record(ctx, tx, w, req.Adoption)
		if err != nil {
			return Record{}, err
		}
		_ = json.Unmarshal(ad.Value, &a)
		e := Execution{Definition: a.Definition, Baseline: d.Baseline, Controller: p.UserID, State: "running", StartedAt: time.Now().UTC(), Inputs: req.Inputs}
		if d.Budget.Seconds > 0 {
			deadline := e.StartedAt.Add(time.Duration(d.Budget.Seconds) * time.Second)
			e.Deadline = &deadline
		}
		for _, n := range d.Nodes {
			for _, name := range n.Inputs {
				if !json.Valid(req.Inputs[n.ID][name]) {
					return Record{}, ErrInvalid
				}
			}
			for name := range req.Inputs[n.ID] {
				if !slices.Contains(n.Inputs, name) {
					return Record{}, ErrInvalid
				}
			}
			t := Task{Node: n.ID, Attempt: 1, Definition: n.Definition, State: "pending"}
			if r, ok := req.Repositories[n.ID]; ok {
				t.Repository = &r
			}
			if err = s.saveTask(ctx, tx, w, ref, t, refs); err != nil {
				return Record{}, err
			}
		}
		return s.saveExecution(ctx, tx, w, ref, e, refs)
	})
}
func (s *Service) Decide(ctx context.Context, p access.Principal, w, id string, req Decision) (apicontract.Response[apicontract.Reference], error) {
	return s.mutate(ctx, p, w, id, "update", "decision", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		e, old, err := execution(ctx, tx, w, id)
		if err != nil {
			return Record{}, err
		}
		if err = s.Access.CheckTx(ctx, tx, p, w, e.Definition.ID, "update"); err != nil {
			return Record{}, err
		}
		d, _, err := s.definition(ctx, tx, p, w, e.Definition)
		if err != nil {
			return Record{}, err
		}
		ts, err := tasks(ctx, tx, w, id)
		if err != nil {
			return Record{}, err
		}
		t, ok := ts[req.Node]
		if !ok || t.Attempt != req.Attempt || t.State != "decision" || (e.State != "waiting" && e.State != "running") {
			return Record{}, ErrState
		}
		var found bool
		for _, n := range d.Nodes {
			if n.ID == t.Node {
				found = slices.Contains(n.Choices, req.Choice)
			}
		}
		if !found {
			return Record{}, ErrInvalid
		}
		now := time.Now().UTC()
		t.State = "done"
		t.Outcome = req.Choice
		t.DecisionBy = p.UserID
		t.DecisionAt = &now
		if err = s.saveTask(ctx, tx, w, ref, t, old.Refs); err != nil {
			return Record{}, err
		}
		e.State = "running"
		e.Reason = ""
		return s.saveExecution(ctx, tx, w, ref, e, old.Refs)
	})
}
func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}
func match(t Task, on []string) bool { return t.State == "done" && slices.Contains(on, t.Outcome) }

// Required predecessors always participate, including when the optional join is any.
func readiness(n Node, ts map[string]Task) (ready, impossible bool) {
	if len(n.Depends) == 0 {
		return true, false
	}
	matches, complete := 0, 0
	for _, dep := range n.Depends {
		t := ts[dep.Node]
		done := t.State == "done" || t.State == "skipped"
		if done {
			complete++
		}
		good := match(t, dep.On)
		if good {
			matches++
		}
		if dep.Required && !good {
			return false, done
		}
	}
	if n.Join == "any" {
		return matches > 0, complete == len(n.Depends) && matches == 0
	}
	return matches == len(n.Depends), complete == len(n.Depends) && matches < len(n.Depends)
}

// beforeStart is also called by the native assigned-input request immediately
// before spawning. Existing non-workflow Runs keep their established behavior.
func (s *Service) beforeStart(ctx context.Context, tx pgx.Tx, w, run string) error {
	var q query = s.Store.Database()
	if tx != nil {
		q = tx
	}
	var id string
	var b []byte
	var current bool
	err := q.QueryRow(ctx, `SELECT execution_id,payload,current FROM workflow_tasks WHERE workspace_id=$1 AND run_id=$2`, w, run).Scan(&id, &b, &current)
	if notFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !current {
		return access.ErrForbidden
	}
	var t Task
	_ = json.Unmarshal(b, &t)
	e, r, err := execution(ctx, q, w, id)
	if err != nil {
		return err
	}
	if (e.State != "running" && e.State != "waiting") || (e.Deadline != nil && time.Now().After(*e.Deadline)) || (t.State != "prepared" && t.State != "running") {
		return access.ErrForbidden
	}
	p := access.Principal{UserID: e.Controller}
	if tx != nil {
		err = s.Access.CheckTx(ctx, tx, p, w, id, "execute")
	} else {
		err = s.Access.Check(ctx, p, w, id, "execute")
	}
	if err != nil {
		return err
	}
	if tx != nil {
		err = s.Access.CheckTx(ctx, tx, p, w, e.Definition.ID, "execute")
	} else {
		err = s.Access.Check(ctx, p, w, e.Definition.ID, "execute")
	}
	if err != nil {
		return err
	}
	if err = s.checkRefs(ctx, q, p, w, append(r.Refs, t.Inputs...)); err != nil {
		return err
	}
	workflow, _, err := s.definition(ctx, q, p, w, e.Definition)
	if err != nil {
		return err
	}
	if workflow.Budget.Cost != nil {
		cost, err := s.cost(ctx, q, p, w, id, *workflow.Budget.Cost)
		if err != nil {
			return err
		}
		if cost.State != "observed" {
			return ErrState
		}
	}
	if t.Definition != nil {
		d, _, err := s.definition(ctx, q, p, w, *t.Definition)
		if err != nil {
			return err
		}
		if d.Write != nil {
			ts, err := tasks(ctx, q, w, id)
			if err != nil {
				return err
			}
			for _, required := range d.Write.Required {
				v := ts[required.Node]
				if !match(v, required.On) {
					return access.ErrForbidden
				}
			}
			if t.Prepared == nil {
				return ErrState
			}
			value, err := selectValue(t.Prepared.Spec.Input.Command.Parameters, d.Write.Parameter)
			if err != nil || string(value) != string(raw(d.Write.Commit)) {
				return ErrState
			}
			if tx == nil {
				if s.Observer == nil {
					return ErrState
				}
				snap, err := s.Observer.Current(ctx, p, w, d.Write.Target)
				if err != nil {
					return err
				}
				if snap.Commit != d.Write.Commit {
					return ErrState
				}
			}
		}
	}
	return nil
}

// A stable operation ID belongs to the persisted task, not to a worker process.
func operation(id, part, version string) apicontract.Mutation {
	return apicontract.Mutation{OperationID: id + ":" + part, ExpectedVersion: version}
}
func accepted(v apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if v.Error != nil || v.Data == nil {
		return Ref{}, ErrState
	}
	return Ref{ID: v.Data.ID, Version: v.Data.Version}, nil
}
func newRun() Ref { return Ref{ID: "run:" + rand.Text(), Version: "0"} }
