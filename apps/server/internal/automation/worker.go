package automation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

const jobColumns = `id,workspace_id,kind,event_id,target_id,target_version,definition_id,definition_version,state,reason,worker,lease,lease_until,attempts,due_at,deadline,checkpoint,result,refs,created_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var result, refs []byte
	err := row.Scan(&j.ID, &j.Workspace, &j.Kind, &j.Event, &j.Target.ID, &j.Target.Version, &j.Definition.ID, &j.Definition.Version, &j.State, &j.Reason, &j.Worker, &j.Lease, &j.LeaseUntil, &j.Attempts, &j.DueAt, &j.Deadline, &j.Checkpoint, &result, &refs, &j.CreatedAt)
	if err == nil {
		err = json.Unmarshal(result, &j.Result)
	}
	if err == nil {
		err = json.Unmarshal(refs, &j.Refs)
	}
	return j, err
}
func logJob(ctx context.Context, tx pgx.Tx, w, id string) error {
	_, err := tx.Exec(ctx, `INSERT INTO automation_job_events(workspace_id,job_id,state,reason,worker,lease,attempt,checkpoint) SELECT workspace_id,id,state,reason,worker,lease,attempts,checkpoint FROM automation_jobs WHERE workspace_id=$1 AND id=$2`, w, id)
	return err
}
func control(ctx context.Context, q query, w, k string) (Control, error) {
	r, err := record(ctx, q, w, Ref{ID: "control:" + k, Version: "head"})
	var c Control
	if err == nil {
		err = json.Unmarshal(r.Value, &c)
	}
	return c, err
}
func (s *Service) controller(ctx context.Context, tx pgx.Tx, j Job, commit bool) (Control, error) {
	c, err := control(ctx, tx, j.Workspace, j.Kind)
	if err != nil {
		return c, access.ErrForbidden
	}
	if !c.Enabled || !c.CanRead || (commit && !c.CanCommit) {
		return c, access.ErrForbidden
	}
	p := access.Principal{UserID: c.Controller}
	if err = s.Access.CheckTx(ctx, tx, p, j.Workspace, "", "manage"); err != nil {
		return c, err
	}
	if err = s.checkRefs(ctx, tx, p, j.Workspace, j.Refs); err != nil {
		return c, err
	}
	return c, nil
}
func (s *Service) authority(ctx context.Context, tx pgx.Tx, j Job, commit bool) (Control, error) {
	c, err := s.controller(ctx, tx, j, commit)
	if err != nil {
		return c, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2 AND version=$3 UNION ALL SELECT 1 FROM shared_changes WHERE workspace_id=$1 AND resource_id=$2 AND version=$3 AND $4='hopper')`, j.Workspace, j.Target.ID, j.Target.Version, j.Kind).Scan(&exists); err != nil || !exists {
		return c, ErrInvalid
	}
	if j.Kind == "triage" {
		if err = s.checkBatch(ctx, tx, j, access.Principal{UserID: c.Controller}, false); err != nil {
			return c, err
		}
		r, reason, _, err := s.selectPolicy(ctx, tx, j.Workspace, j.Target)
		if err != nil || reason != "" || r.Ref != j.Definition {
			return c, ErrInvalid
		}
	}
	return c, nil
}
func (s *Service) budget(ctx context.Context, tx pgx.Tx, j Job, c Control, reserved bool) (string, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM automation_job_events e JOIN automation_jobs j ON j.workspace_id=e.workspace_id AND j.id=e.job_id WHERE e.workspace_id=$1 AND j.kind=$2 AND e.state='running' AND e.at>now()-make_interval(secs=>$3)`, j.Workspace, j.Kind, c.PeriodSeconds).Scan(&count); err != nil {
		return "", err
	}
	limit := c.MaxRequests
	if reserved {
		limit++
	}
	if count >= limit {
		return "request_budget", nil
	}
	if c.Budget == nil {
		return "", nil
	}
	if j.Kind == "triage" {
		return s.triageBudget(ctx, tx, j, c)
	}
	var missing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_jobs WHERE workspace_id=$1 AND kind=$2 AND updated_at>now()-make_interval(secs=>$3) AND result<>'null'::jsonb AND result->>'usage_state' NOT IN ('complete','not_applicable'))`, j.Workspace, j.Kind, c.PeriodSeconds).Scan(&missing); err != nil {
		return "", err
	}
	if missing {
		return "usage_missing", nil
	}
	// Fixed, unique evaluations are one valuation/currency, never a sum of
	// reported + estimated or a double count of retries' shared references.
	var unavailable, exceeded bool
	err := tx.QueryRow(ctx, `WITH used AS (
 SELECT DISTINCT r.id,r.version FROM automation_jobs j CROSS JOIN LATERAL jsonb_to_recordset(COALESCE(j.result->'usage','[]')) AS r(id text,version text)
 WHERE j.workspace_id=$1 AND j.kind=$4 AND j.updated_at>now()-make_interval(secs=>$5)), basis AS (
 SELECT u.id,u.version,r.payload,r.dependencies||jsonb_build_array(jsonb_build_object('id',u.id,'version',u.version)) AS refs FROM used u LEFT JOIN usage_records r ON r.workspace_id=$1 AND r.id=u.id AND r.version=u.version)
 SELECT EXISTS(SELECT 1 FROM basis e WHERE payload IS NULL OR NOT (`+access.ReadFilterSQL("e.refs")+`) OR payload->>'state'<>'rated'),
 COALESCE((SELECT sum((payload->>'amount')::numeric) FROM basis WHERE payload->>'kind'=$6 AND payload->>'currency'=$7),0)>=$8::numeric`, j.Workspace, c.Controller, "", j.Kind, c.PeriodSeconds, c.Budget.Valuation, c.Budget.Currency, c.Budget.Amount).Scan(&unavailable, &exceeded)
	if err != nil {
		return "", err
	}
	if unavailable {
		return "usage_unavailable", nil
	}
	if exceeded {
		return "cost_budget", nil
	}
	return "", nil
}

// Claim serializes only a Workspace/kind's short reservation, with separate
// Hopper and Triage slots across all Worker processes sharing PostgreSQL.
func (s *Service) Claim(ctx context.Context, k, worker string) (*Job, error) {
	if (k != "hopper" && k != "triage") || worker == "" {
		return nil, ErrInvalid
	}
	var claimed *Job
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id,id FROM automation_jobs WHERE kind=$1 AND (state IN ('waiting','retry') OR (state='running' AND lease_until<=now())) AND due_at<=now() ORDER BY due_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, k)
		if err != nil {
			return err
		}
		ids := [][2]string{}
		for rows.Next() {
			var w, id string
			if err = rows.Scan(&w, &id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, [2]string{w, id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+" FROM automation_jobs WHERE workspace_id=$1 AND id=$2", id[0], id[1]))
			if err != nil {
				return err
			}
			var lock bool
			if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,15915))", j.Workspace+":"+k).Scan(&lock); err != nil {
				return err
			}
			if !lock {
				continue
			}
			c, authErr := s.authority(ctx, tx, j, false)
			reason := ""
			if authErr != nil {
				reason = "authority_revoked"
				if errors.Is(authErr, ErrInvalid) {
					reason = "basis_changed"
				}
			}
			if reason == "" && j.Kind == "triage" {
				err := s.checkBatch(ctx, tx, j, access.Principal{UserID: c.Controller}, true)
				var wait triageWait
				if errors.As(err, &wait) {
					if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET state='waiting',reason=$3,due_at=now()+interval '5 seconds',lease_until=NULL WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, string(wait)); err != nil {
						return err
					}
					if j.State != "waiting" || j.Reason != string(wait) {
						if err = logJob(ctx, tx, j.Workspace, j.ID); err != nil {
							return err
						}
					}
					continue
				}
				if err != nil {
					return err
				}
			}
			if reason == "" && (j.Attempts >= c.MaxAttempts || (j.Deadline != nil && !time.Now().Before(*j.Deadline))) {
				reason = "deadline_or_attempts"
			}
			if reason == "" {
				reason, err = s.budget(ctx, tx, j, c, false)
				if err != nil {
					return err
				}
			}
			if reason != "" {
				if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET state='blocked',reason=$3,lease_until=NULL,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, reason); err != nil {
					return err
				}
				if err = logJob(ctx, tx, j.Workspace, j.ID); err != nil {
					return err
				}
				continue
			}
			var active int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM automation_jobs WHERE workspace_id=$1 AND kind=$2 AND state='running' AND lease_until>now()`, j.Workspace, k).Scan(&active); err != nil {
				return err
			}
			if active >= c.Slots {
				continue
			}
			j, err = scanJob(tx.QueryRow(ctx, `UPDATE automation_jobs SET state='running',reason='',worker=$3,lease=$4,lease_until=now()+make_interval(secs=>$5),attempts=attempts+1,deadline=COALESCE(deadline,now()+make_interval(secs=>$6)),updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING `+jobColumns, j.Workspace, j.ID, worker, rand.Text(), c.LeaseSeconds, c.MaxAttempts*c.LeaseSeconds+(c.MaxAttempts-1)*c.RetrySeconds))
			if err != nil {
				return err
			}
			if err = logJob(ctx, tx, j.Workspace, j.ID); err != nil {
				return err
			}
			claimed = &j
			return nil
		}
		return nil
	})
	return claimed, err
}
func sameLease(ctx context.Context, tx pgx.Tx, j Job) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT state='running' AND lease=$3 AND lease_until>now() FROM automation_jobs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, j.Workspace, j.ID, j.Lease).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrLease
	}
	return nil
}
func (s *Service) Checkpoint(ctx context.Context, j Job, value json.RawMessage) error {
	if !json.Valid(value) {
		return ErrInvalid
	}
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := sameLease(ctx, tx, j); err != nil {
			return err
		}
		if _, err := s.authority(ctx, tx, j, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE automation_jobs SET checkpoint=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, value); err != nil {
			return err
		}
		return logJob(ctx, tx, j.Workspace, j.ID)
	})
}
func (s *Service) retry(ctx context.Context, j Job, cause string) error {
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := sameLease(ctx, tx, j); err != nil {
			return err
		}
		c, err := control(ctx, tx, j.Workspace, j.Kind)
		if err != nil {
			return err
		}
		state := "retry"
		if j.Attempts >= c.MaxAttempts || (j.Deadline != nil && !time.Now().Before(*j.Deadline)) {
			state = "failed"
		}
		if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET state=$3,reason=$4,due_at=now()+make_interval(secs=>$5),lease_until=NULL,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, state, cause, c.RetrySeconds); err != nil {
			return err
		}
		return logJob(ctx, tx, j.Workspace, j.ID)
	})
}

type Executor func(context.Context, Job) (Result, error)

func (s *Service) RunOne(ctx context.Context, k, worker string, execute Executor) (bool, error) {
	if execute == nil {
		return false, ErrInvalid
	}
	j, err := s.Claim(ctx, k, worker)
	if err != nil || j == nil {
		return false, err
	}
	c, err := control(ctx, s.Store.Database(), j.Workspace, k)
	if err != nil {
		return true, err
	}
	deadline := time.Now().Add(time.Duration(c.TimeoutSeconds) * time.Second)
	if j.LeaseUntil.Before(deadline) {
		deadline = *j.LeaseUntil
	}
	if j.Deadline.Before(deadline) {
		deadline = *j.Deadline
	}
	run, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var result Result
	if j.Result != nil {
		result = *j.Result
	} else {
		result, err = execute(run, *j)
	}
	if err != nil {
		var wait triageWait
		if j.Kind == "triage" && errors.As(err, &wait) {
			return true, s.deferTriage(ctx, *j, string(wait))
		}
		return true, s.retry(ctx, *j, "executor_failed")
	}
	return true, s.Finish(ctx, *j, result)
}

func (s *Service) Finish(ctx context.Context, j Job, result Result) error {
	if result.Refs == nil {
		result.Refs = []Ref{}
	}
	if result.Usage == nil {
		result.Usage = []Ref{}
	}
	if result.UsageState != "complete" && result.UsageState != "missing" && result.UsageState != "not_applicable" {
		return ErrInvalid
	}
	// Retain the returned result before attempting shared-state CAS. Reclaim can
	// finish a saved result without repeating the external call.
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := sameLease(ctx, tx, j); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE automation_jobs SET result=$3 WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, encode(result))
		return err
	})
	if err != nil {
		return err
	}
	id := "priority:" + j.Target.ID
	if j.Kind == "hopper" {
		return s.finishHopper(ctx, j, result)
	}
	var expected string
	if err = s.Store.Database().QueryRow(ctx, "SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')", j.Workspace, id).Scan(&expected); err != nil {
		return err
	}
	c, err := control(ctx, s.Store.Database(), j.Workspace, j.Kind)
	if err != nil {
		return err
	}
	scope := s.workerScope(ctx, j, c.Controller)
	response, err := s.Store.Apply(ctx, scope, s.workerCommand(j, id, expected, result), func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		if err := sameLease(ctx, tx, j); err != nil {
			return nil, err
		}
		if _, err := s.authority(ctx, tx, j, true); err != nil {
			return nil, err
		}
		policy, err := record(ctx, tx, j.Workspace, j.Definition)
		if err != nil {
			return nil, err
		}
		var definition Policy
		_ = json.Unmarshal(policy.Value, &definition)
		if result.Priority != nil && !hasLevel(definition, *result.Priority) {
			return nil, ErrInvalid
		}
		p := Priority{Target: j.Target, Policy: j.Definition, State: "unevaluated"}
		old, err := record(ctx, tx, j.Workspace, Ref{ID: id, Version: "head"})
		if err == nil {
			_ = json.Unmarshal(old.Value, &p)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if p.Adopted != nil && p.AdoptedPolicy == nil {
			basis := p.Policy
			p.AdoptedPolicy = &basis
		}
		p.Target = j.Target
		p.Policy = j.Definition
		p.Recommendation = result.Priority
		p.RecommendationTarget = &j.Target
		p.RecommendationPolicy = &j.Definition
		p.Reason = result.Reason
		p.RecommendationReason = result.Reason
		p.Job = j.ID
		p.Refs = append(append([]Ref{}, result.Refs...), result.Usage...)
		p.State = "evaluated"
		if result.State != "" {
			p.State = result.State
		}
		p.Basis = result.Basis
		if result.Basis != "" {
			if definition.Model != nil {
				connection, err := s.Gateway.Connection(j.Workspace, definition.Model.Connection, definition.Model.Revision)
				if err != nil || !connection.AllowsChain(definition.Model.Model, definition.Model.Fallbacks, definition.Model.Allowed) {
					return nil, access.ErrForbidden
				}
			}
			rec, err := record(ctx, tx, j.Workspace, Ref{ID: "triage-input:" + j.ID, Version: result.Basis})
			if err != nil {
				return nil, err
			}
			var input TriageInput
			if json.Unmarshal(rec.Value, &input) != nil {
				return nil, ErrInvalid
			}
			if err = s.inputCurrent(ctx, tx, j, input); err != nil {
				return nil, err
			}
			if err = s.triageRefs(ctx, tx, access.Principal{UserID: c.Controller, OperationID: input.Operation}, j.Workspace, rec.Refs); err != nil {
				return nil, err
			}
		}
		if result.Priority == nil && result.State == "" {
			p.State = "information_missing"
		}
		activation, err := record(ctx, tx, j.Workspace, Ref{ID: "activation:" + strings.TrimPrefix(j.Definition.ID, "policy:"), Version: "head"})
		if err != nil {
			return nil, err
		}
		var active Activation
		_ = json.Unmarshal(activation.Value, &active)
		if !p.Fixed && active.AutoApply && result.Priority != nil {
			p.Adopted = result.Priority
			p.AdoptedPolicy = &j.Definition
			p.Origin = "triage"
			p.AdoptedReason = result.Reason
		}
		refs := append(append([]Ref{}, j.Refs...), p.Refs...)
		if err = s.checkRefs(ctx, tx, access.Principal{UserID: c.Controller}, j.Workspace, refs); err != nil {
			return nil, err
		}
		if expected == "0" {
			if err = s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: j.Workspace, ID: id}, nil); err != nil {
				return nil, err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE automation_jobs SET state='done',reason='',lease_until=NULL,refs=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, encode(refs)); err != nil {
			return nil, err
		}
		if err = logJob(ctx, tx, j.Workspace, j.ID); err != nil {
			return nil, err
		}
		return s.save(ctx, tx, j.Workspace, Ref{ID: id, Version: ref.Version}, "priority", p, refs)
	})
	if err != nil {
		return err
	}
	if response.Error != nil {
		if response.Error.Code == "forbidden" {
			return s.block(ctx, j, "authority_revoked")
		}
		return ErrLease
	}
	return nil
}

func (s *Service) workerScope(ctx context.Context, j Job, controller string) persistence.Scope {
	return persistence.Scope{ActorID: "automation:" + j.Kind + ":" + controller, Grant: apicontract.Grant{WorkspaceID: j.Workspace, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := sameLease(ctx, tx, j); err != nil {
			return problem(access.ErrForbidden)
		}
		_, err := s.authority(ctx, tx, j, true)
		return problem(err)
	}}
}
func (s *Service) workerCommand(j Job, id, expected string, result Result) persistence.Command {
	return persistence.Command{WorkspaceID: j.Workspace, ResourceID: id, Kind: "automation-result", Mutation: apicontract.Mutation{OperationID: "finish:" + j.ID + ":" + j.Lease, ExpectedVersion: expected}, Payload: encode(result)}
}
func (s *Service) block(ctx context.Context, j Job, reason string) error {
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := sameLease(ctx, tx, j); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE automation_jobs SET state='blocked',reason=$3,lease_until=NULL,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, reason); err != nil {
			return err
		}
		return logJob(ctx, tx, j.Workspace, j.ID)
	})
}
func (s *Service) finishHopper(ctx context.Context, j Job, result Result) error {
	c, err := control(ctx, s.Store.Database(), j.Workspace, j.Kind)
	if err != nil {
		return err
	}
	response, err := s.Store.Apply(ctx, s.workerScope(ctx, j, c.Controller), s.workerCommand(j, "job-result:"+j.ID, "0", result), func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		refs := append(append(append([]Ref{}, j.Refs...), result.Refs...), result.Usage...)
		if err := s.checkRefs(ctx, tx, access.Principal{UserID: c.Controller}, j.Workspace, refs); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE automation_jobs SET state='done',reason='',lease_until=NULL,refs=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, encode(refs)); err != nil {
			return nil, err
		}
		if err := logJob(ctx, tx, j.Workspace, j.ID); err != nil {
			return nil, err
		}
		return encode(result), nil
	})
	if err != nil {
		return err
	}
	if response.Error != nil {
		return s.block(ctx, j, "authority_revoked")
	}
	return nil
}
