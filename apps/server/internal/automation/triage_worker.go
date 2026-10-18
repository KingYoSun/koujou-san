package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

type TriageAttempt struct {
	Job      string              `json:"job"`
	Input    Ref                 `json:"input"`
	Number   int                 `json:"number"`
	Actor    string              `json:"actor"`
	Response *serverllm.Response `json:"response,omitempty"`
	Decision *Decision           `json:"decision,omitempty"`
	State    string              `json:"state"`
	Reason   string              `json:"reason"`
	Usage    []Ref               `json:"usage"`
	Costs    []Ref               `json:"costs"`
}

func (s *Service) observeTriage(ctx context.Context, tx pgx.Tx, j Job, input Record, p Policy, attempt TriageAttempt, intent Ref, reply modelreply.Reply, response *serverllm.Response, provisional bool) ([]Ref, []Ref, error) {
	deps := []usage.Ref{}
	for _, ref := range append(append([]Ref{}, input.Refs...), input.Ref, intent) {
		deps = append(deps, usage.Ref{ID: ref.ID, Version: ref.Version})
	}
	guard := usage.OwnerWrite{Actor: "automation:triage", Dependencies: deps, Reserved: !provisional, Check: func(ctx context.Context, tx pgx.Tx) error {
		var present bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_model_attempts WHERE workspace_id=$1 AND resource_id=$2 AND input_id=$3 AND input_version=$4 AND actor=$5)`, j.Workspace, intent.ID, input.ID, input.Version, attempt.Actor).Scan(&present)
		if err == nil && !present {
			return access.ErrForbidden
		}
		return err
	}}
	return serverllm.Observe(ctx, tx, access.Principal{UserID: attempt.Actor}, j.Workspace, "triage", j.ID, "triage-observe:"+intent.ID+fmt.Sprint(provisional), intent, serverllm.MeterInput{ID: input.ID, Version: input.Version, Refs: input.Refs}, p.Model, reply, response, provisional, s.Usage, guard)
}

func (s *Service) triageResult(ctx context.Context, j Job, input Record, attempt TriageAttempt, response Ref) (Result, error) {
	result := Result{State: attempt.State, Reason: attempt.Reason, Basis: input.Version, Refs: append(append([]Ref{}, input.Refs...), input.Ref, response), Usage: []Ref{}, UsageState: "not_applicable"}
	if attempt.Decision != nil {
		result.Priority = attempt.Decision.Priority
		result.Reason = attempt.Decision.Reason
	}
	// Consumption belongs to the evaluation even if its value was never adopted.
	rows, err := s.Store.Database().Query(ctx, `SELECT a.resource_id,COALESCE(r.payload,'{}'::jsonb),u.resource_id,h.version FROM triage_model_attempts a LEFT JOIN automation_records r ON r.workspace_id=a.workspace_id AND r.id=a.resource_id AND r.version=a.response_version LEFT JOIN usage_events u ON u.workspace_id=a.workspace_id AND u.event_id=a.resource_id AND u.source LIKE 'server-model:%' LEFT JOIN shared_versions h ON h.workspace_id=u.workspace_id AND h.resource_id=u.resource_id WHERE a.workspace_id=$1 AND a.job_id=$2 ORDER BY a.number`, j.Workspace, j.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		var usageID, version *string
		if err = rows.Scan(&id, &raw, &usageID, &version); err != nil {
			return result, err
		}
		var old TriageAttempt
		_ = json.Unmarshal(raw, &old)
		if result.UsageState == "not_applicable" {
			result.UsageState = "complete"
		}
		if usageID != nil && version != nil {
			result.Refs = append(result.Refs, Ref{ID: *usageID, Version: *version})
		}
		if len(old.Costs) == 0 {
			result.UsageState = "missing"
		}
		result.Usage = append(result.Usage, old.Costs...)
	}
	return result, rows.Err()
}

func (s *Service) Execute(ctx context.Context, j Job) (Result, error) {
	if j.Kind != "triage" {
		return Result{}, ErrInvalid
	}
	if err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error { _, _, err := s.CheckJobTx(ctx, tx, j); return err }); err != nil {
		var wait triageWait
		if errors.As(err, &wait) {
			return Result{}, err
		}
		if errors.Is(err, access.ErrForbidden) {
			return Result{State: "blocked", Reason: "automation_authority_or_budget", Refs: j.Refs, Usage: []Ref{}, UsageState: "not_applicable"}, nil
		}
		return Result{}, err
	}
	var p Policy
	policy, err := record(ctx, s.Store.Database(), j.Workspace, j.Definition)
	if err != nil {
		return Result{}, err
	}
	if json.Unmarshal(policy.Value, &p) != nil {
		return Result{}, ErrInvalid
	}
	if p.Model == nil {
		return Result{State: "input_waiting", Reason: "model_unset", Refs: []Ref{}, Usage: []Ref{}, UsageState: "not_applicable"}, nil
	}
	input, frozen, err := s.prepareTriageInput(ctx, j, p)
	if err != nil {
		return Result{}, err
	}
	attempt := TriageAttempt{Job: j.ID, Input: input.Ref, Number: j.Attempts, State: frozen.State, Reason: frozen.Reason, Usage: []Ref{}, Costs: []Ref{}}
	if frozen.State != "ready" {
		return Result{State: frozen.State, Reason: frozen.Reason, Basis: input.Version, Refs: append(input.Refs, input.Ref), Usage: []Ref{}, UsageState: "not_applicable"}, nil
	}
	// A completed response can be committed under a new lease without another call.
	var responseRef Ref
	err = s.Store.Database().QueryRow(ctx, `SELECT resource_id,response_version FROM triage_model_attempts WHERE workspace_id=$1 AND job_id=$2 AND response_version IS NOT NULL ORDER BY number DESC LIMIT 1`, j.Workspace, j.ID).Scan(&responseRef.ID, &responseRef.Version)
	if err == nil {
		rec, err := record(ctx, s.Store.Database(), j.Workspace, responseRef)
		if err != nil {
			return Result{}, err
		}
		if json.Unmarshal(rec.Value, &attempt) != nil {
			return Result{}, ErrInvalid
		}
		if attempt.State != "failed" {
			return s.triageResult(ctx, j, input, attempt, responseRef)
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	connection, err := s.Gateway.Connection(j.Workspace, p.Model.Connection, p.Model.Revision)
	if err != nil || !connection.AllowsChain(p.Model.Model, p.Model.Fallbacks, p.Model.Allowed) {
		return Result{State: "blocked", Reason: "model_connection_unavailable", Basis: input.Version, Refs: append(input.Refs, input.Ref), Usage: []Ref{}, UsageState: "not_applicable"}, nil
	}
	id := "triage-model:" + j.ID + ":" + fmt.Sprint(j.Attempts)
	var maxAttempts int
	check := func(tx pgx.Tx) error {
		c, _, err := s.CheckJobTx(ctx, tx, j)
		if err != nil {
			return err
		}
		maxAttempts = c.MaxAttempts
		attempt.Actor = c.Controller
		if err = s.inputCurrent(ctx, tx, j, frozen); err != nil {
			return err
		}
		actor := access.Principal{UserID: c.Controller, OperationID: frozen.Operation}
		if err = s.triageRefs(ctx, tx, actor, j.Workspace, input.Refs); err != nil {
			return err
		}
		if p.Model.Cost != nil {
			cost, err := usage.ObserveCosts(ctx, tx, access.Principal{UserID: c.Controller}, j.Workspace, "triage", func() []string {
				var exists bool
				_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_model_attempts WHERE workspace_id=$1 AND job_id=$2)`, j.Workspace, j.ID).Scan(&exists)
				if exists {
					return []string{j.ID}
				}
				return []string{}
			}(), *p.Model.Cost)
			if err != nil {
				return err
			}
			if cost.State != "observed" {
				return access.ErrForbidden
			}
		}
		return nil
	}
	intent, err := s.systemTriage(ctx, j.Workspace, id, "model-attempt", "intent:"+id, "0", check, func(tx pgx.Tx, ref Ref) (Record, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO triage_model_attempts(workspace_id,job_id,number,resource_id,input_id,input_version,lease,actor) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, j.Workspace, j.ID, j.Attempts, id, input.ID, input.Version, j.Lease, attempt.Actor); err != nil {
			return Record{}, err
		}
		if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: j.Workspace, ID: ref.ID}, nil); err != nil {
			return Record{}, err
		}
		if _, err := s.save(ctx, tx, j.Workspace, ref, "model-attempt", attempt, append(input.Refs, input.Ref)); err != nil {
			return Record{}, err
		}
		_, _, err := s.observeTriage(ctx, tx, j, input, p, attempt, ref, modelreply.Reply{}, nil, true)
		return Record{Ref: ref}, err
	})
	if err != nil {
		return Result{}, err
	}
	err = serverllm.Dispatch(ctx, connection, p.Model, frozen.Request, id, func(saveCtx context.Context, response serverllm.Response, reply modelreply.Reply, reason string) error {
		attempt.Response = &response
		attempt.State = "evaluated"
		attempt.Reason = reason
		if reason != "" {
			attempt.State = "failed"
		} else {
			var fields map[string]json.RawMessage
			var decision Decision
			content := reply.Choices[0].Message.Content
			if len(reply.Choices[0].Message.Calls) > 0 || content == nil || json.Unmarshal([]byte(*content), &fields) != nil || fields["priority"] == nil || decode([]byte(*content), &decision) != nil || !decision.valid(frozen, p) {
				attempt.State = "failed"
				attempt.Reason = "invalid_triage_response"
			} else {
				attempt.Decision = &decision
				attempt.State = decision.State
				attempt.Reason = decision.Reason
			}
		}
		reserved := func(tx pgx.Tx) error {
			var exists bool
			err := tx.QueryRow(saveCtx, `SELECT EXISTS(SELECT 1 FROM triage_model_attempts WHERE workspace_id=$1 AND resource_id=$2 AND lease=$3 AND actor=$4 AND input_version=$5)`, j.Workspace, id, j.Lease, attempt.Actor, input.Version).Scan(&exists)
			if err == nil && !exists {
				return access.ErrForbidden
			}
			return err
		}
		rec, err := s.systemTriage(saveCtx, j.Workspace, id, "model-attempt", "response:"+id, intent.Version, reserved, func(tx pgx.Tx, ref Ref) (Record, error) {
			var err error
			attempt.Usage, attempt.Costs, err = s.observeTriage(saveCtx, tx, j, input, p, attempt, intent.Ref, reply, &response, false)
			if err != nil {
				return Record{}, err
			}
			refs := append(append(append([]Ref{}, input.Refs...), input.Ref), append(attempt.Usage, attempt.Costs...)...)
			if _, err = s.save(saveCtx, tx, j.Workspace, ref, "model-attempt", attempt, refs); err != nil {
				return Record{}, err
			}
			_, err = tx.Exec(saveCtx, `UPDATE triage_model_attempts SET response_version=$3 WHERE workspace_id=$1 AND resource_id=$2`, j.Workspace, id, ref.Version)
			return Record{Ref: ref}, err
		})
		responseRef = rec.Ref
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if attempt.State == "failed" && j.Attempts < maxAttempts {
		return Result{}, errors.New("retry_triage_request")
	}
	return s.triageResult(ctx, j, input, attempt, responseRef)
}

func (s *Service) Start(ctx context.Context) {
	loop := func(fn func()) {
		go func() {
			timer := time.NewTicker(500 * time.Millisecond)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					fn()
				}
			}
		}()
	}
	loop(func() {
		_, _ = s.PumpTriage(ctx, time.Now().UTC())
		_, _ = s.BindPublication(ctx)
		_, _ = s.SyncPriority(ctx)
	})
	for worker := 0; worker < 2; worker++ {
		id := fmt.Sprint("server-triage-", worker)
		loop(func() { _, _ = s.RunOne(ctx, "triage", id, s.Execute) })
	}
}

func (s *Service) triageBudget(ctx context.Context, tx pgx.Tx, j Job, c Control) (string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT job_id FROM triage_model_attempts WHERE workspace_id=$1 AND created_at>now()-make_interval(secs=>$2)`, j.Workspace, c.PeriodSeconds)
	if err != nil {
		return "", err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	cost, err := usage.ObserveCosts(ctx, tx, access.Principal{UserID: c.Controller}, j.Workspace, "triage", ids, usage.CostBudget{Amount: c.Budget.Amount, Currency: c.Budget.Currency, Valuation: c.Budget.Valuation})
	if err != nil {
		return "", err
	}
	if cost.State != "observed" {
		return "usage_" + cost.State, nil
	}
	return "", nil
}

// A stopped batch or unknown consumption keeps its accepted request pending.
// An unsent reservation is not a model attempt; a saved result keeps its count.
func (s *Service) deferTriage(ctx context.Context, j Job, reason string) error {
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		if err := sameLease(ctx, tx, j); err != nil {
			return err
		}
		var sent bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM triage_model_attempts WHERE workspace_id=$1 AND job_id=$2 AND number=$3)`, j.Workspace, j.ID, j.Attempts).Scan(&sent); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE automation_jobs SET state='waiting',reason=$3,due_at=now()+interval '5 seconds',lease_until=NULL,attempts=attempts-CASE WHEN $4 THEN 0 ELSE 1 END,deadline=CASE WHEN NOT $4 AND attempts=1 THEN NULL ELSE deadline END,updated_at=now() WHERE workspace_id=$1 AND id=$2`, j.Workspace, j.ID, reason, sent)
		if err != nil {
			return err
		}
		return logJob(ctx, tx, j.Workspace, j.ID)
	})
}
