package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
)

func TestAutomationPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:automation")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.Database().Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"admin", "editor", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, id)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider'); INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','admin','admin'),('one','editor','editor'),('one','viewer','viewer'),('two','outsider','admin')`)
	a := access.New(store, []byte(strings.Repeat("k", 32)), nil)
	s := New(store, a)
	c := contexts.New(store, a, contexts.DefaultLimits())
	admin := access.Principal{UserID: "admin"}
	editor := access.Principal{UserID: "editor"}
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("op-", serial), ExpectedVersion: version}
	}
	put := func(id, version string, value any) Ref {
		t.Helper()
		r, e := s.Put(ctx, admin, "one", id, Write{Mutation: mutation(version), Value: encode(value)})
		if e != nil || r.Data == nil {
			t.Fatal(id, r, e)
		}
		return Ref{ID: r.Data.ID, Version: r.Data.Version}
	}
	contextPut := func(kind, id, version string, value any) (Ref, error) {
		r, e := c.Put(ctx, editor, "one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: mutation(version), Value: encode(value)})
		if e != nil {
			return Ref{}, e
		}
		if r.Data == nil {
			return Ref{}, fmt.Errorf("context write %v", r.Error)
		}
		return Ref{ID: r.Data.ID, Version: r.Data.Version}, nil
	}
	scope := func(id, version, parent string) Ref {
		t.Helper()
		v := contexts.Scope{Kind: "issue", Name: id}
		if parent != "" {
			v.Parent = &contexts.Identity{Kind: "milestone", ID: parent}
		}
		r, e := contextPut("scope", id, version, v)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	parent, e := contextPut("scope", "milestone", "0", contexts.Scope{Kind: "milestone", Name: "milestone"})
	if e != nil {
		t.Fatal(e)
	}
	issue := scope("issue", "0", "milestone")
	status, e := s.Priority(ctx, admin, "one", issue.ID)
	if e != nil || !strings.Contains(string(status.Value), "policy_unset") {
		t.Fatal(status, e)
	}
	policy := Policy{Name: "first", Scopes: []string{"milestone"}, Levels: DefaultLevels(), Criteria: "fixture criteria", Context: []Ref{}}
	var countBefore int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs").Scan(&countBefore)
	p := put("policy:first", "0", policy)
	put("activation:first", "0", Activation{Policy: p, Enabled: true, AutoApply: true})
	var countAfter int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs").Scan(&countAfter)
	if countBefore != countAfter {
		t.Fatal("save/enable queued old targets")
	}
	controlValue := Control{Kind: "triage", Enabled: true, CanRead: true, CanCommit: true, Slots: 1, MaxAttempts: 3, LeaseSeconds: 5, TimeoutSeconds: 3, RetrySeconds: 0, MaxRequests: 1000, PeriodSeconds: 60}
	ctl := put("control:triage", "0", controlValue)
	chosen, reason, e := s.Select(ctx, admin, "one", issue)
	if e != nil || reason != "" || chosen.Ref != p {
		t.Fatal(chosen, reason, e)
	}
	// Rollback after the outbox insert cannot leave the source or job committed.
	accepted := store.Accepted
	store.Accepted = func(ctx context.Context, tx pgx.Tx, cmd persistence.Command, ref apicontract.Reference, raw json.RawMessage) error {
		if e := accepted(ctx, tx, cmd, ref, raw); e != nil {
			return e
		}
		return errors.New("fixture stop before commit")
	}
	_, e = contextPut("scope", "issue", issue.Version, contexts.Scope{Kind: "issue", Name: "stopped", Parent: &contexts.Identity{Kind: "milestone", ID: "milestone"}})
	if e == nil {
		t.Fatal("fault did not roll back")
	}
	store.Accepted = accepted
	var current string
	_ = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='one' AND resource_id=$1", issue.ID).Scan(&current)
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs").Scan(&countAfter)
	if current != issue.Version || countAfter != countBefore {
		t.Fatal("source/outbox split")
	}
	issue = scope("issue", issue.Version, "milestone")
	job, e := s.Claim(ctx, "triage", "worker-one")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	other, e := s.Claim(ctx, "triage", "worker-two")
	if e != nil || other != nil {
		t.Fatal("slot duplicated", other, e)
	}
	if e = s.Checkpoint(ctx, *job, encode(map[string]string{"step": "input-read"})); e != nil {
		t.Fatal(e)
	}
	value := "P1"
	if e = s.Finish(ctx, *job, Result{Priority: &value, Reason: "fixture", UsageState: "not_applicable"}); e != nil {
		t.Fatal(e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	var priority Priority
	_ = decode(status.Value, &priority)
	if priority.Adopted == nil || *priority.Adopted != "P1" {
		t.Fatal(string(status.Value))
	}
	manual := "P2"
	priorityRef := put(status.ID, status.Version, PriorityEdit{Target: issue, Policy: p, Value: &manual, Reason: "human choice"})
	issue = scope("issue", issue.Version, "milestone")
	job, e = s.Claim(ctx, "triage", "worker-one")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	value = "P0"
	if e = s.Finish(ctx, *job, Result{Priority: &value, Reason: "latest recommendation", UsageState: "not_applicable"}); e != nil {
		t.Fatal(e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if !priority.Fixed || *priority.Adopted != "P2" || priority.Recommendation == nil || *priority.Recommendation != "P0" {
		t.Fatal(string(status.Value))
	}
	priorityRef = put(status.ID, status.Version, PriorityEdit{Target: issue, Policy: p, Unlock: true, Reason: "return to automation"})
	status, e = s.Read(ctx, admin, "one", priorityRef)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if priority.Fixed || *priority.Adopted != "P0" {
		t.Fatal(string(status.Value))
	}
	// Follow the GUI's selection -> displayed priority -> edit path after a
	// source update. The saved basis remains old; editing uses the current input.
	editDisplayedPriority := func() {
		t.Helper()
		selection, err := s.Query(ctx, admin, "one", Request{Mode: "selection", ID: issue.ID})
		if err != nil || selection.Data == nil || len(selection.Data.Items) != 1 {
			t.Fatal(selection, err)
		}
		var choice struct {
			Target Ref    `json:"target"`
			Policy Record `json:"policy"`
		}
		_ = decode(selection.Data.Items[0], &choice)
		displayed, err := s.Priority(ctx, admin, "one", issue.ID)
		if err != nil {
			t.Fatal(err)
		}
		if choice.Target != issue {
			t.Fatal("GUI selection lost current target", choice.Target, issue)
		}
		manualValue := "P0"
		ref := put(displayed.ID, displayed.Version, PriorityEdit{Target: choice.Target, Policy: choice.Policy.Ref, Value: &manualValue, Reason: "manual while evaluation unavailable"})
		ref = put(ref.ID, ref.Version, PriorityEdit{Target: choice.Target, Policy: choice.Policy.Ref, Unlock: true, Reason: "unlock without adopting stale recommendation"})
		saved, err := s.Read(ctx, admin, "one", ref)
		var v Priority
		_ = decode(saved.Value, &v)
		if err != nil || v.Fixed || v.Adopted == nil || *v.Adopted != "P0" || v.Target != issue {
			t.Fatal("manual/unlock during evaluation wait or failure", string(saved.Value), err)
		}
	}
	// Missing input, failure and stopping retain the adopted value and expose a
	// distinct processing state instead of manufacturing the lowest level.
	issue = scope("issue", issue.Version, "milestone")
	editDisplayedPriority()
	job, e = s.Claim(ctx, "triage", "missing-input-worker")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	if e = s.Finish(ctx, *job, Result{Reason: "input unavailable", UsageState: "not_applicable"}); e != nil {
		t.Fatal(e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if priority.State != "information_missing" || priority.Adopted == nil || *priority.Adopted != "P0" {
		t.Fatal(string(status.Value))
	}
	controlValue.MaxAttempts = 1
	ctl = put(ctl.ID, ctl.Version, controlValue)
	issue = scope("issue", issue.Version, "milestone")
	job, e = s.Claim(ctx, "triage", "failing-worker")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	if e = s.retry(ctx, *job, "executor_failed"); e != nil {
		t.Fatal(e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if priority.State != "failed" || !priority.Stale || *priority.Adopted != "P0" {
		t.Fatal(string(status.Value))
	}
	editDisplayedPriority()
	controlValue.MaxAttempts = 3
	ctl = put(ctl.ID, ctl.Version, controlValue)
	issue = scope("issue", issue.Version, "milestone")
	job, e = s.Claim(ctx, "triage", "stopped-worker")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	stopped, e := s.JobAction(ctx, admin, "one", job.ID, JobAction{Mutation: mutation("0"), Action: "stop", Reason: "fixture explicit stop"})
	if e != nil || stopped.Data == nil {
		t.Fatal(stopped, e)
	}
	if e = s.Checkpoint(ctx, *job, encode("late checkpoint")); !errors.Is(e, ErrLease) {
		t.Fatal("stopped owner wrote checkpoint", e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if priority.State != "stopped" || *priority.Adopted != "P0" {
		t.Fatal(string(status.Value))
	}
	// A higher-precedence definition is selected; ties are waiting, not merged.
	policy.Name = "second"
	policy.Precedence = 1
	policy.Levels = []Level{{"now", "Now", ""}, {"later", "Later", ""}}
	p2 := put("policy:second", "0", policy)
	active2 := put("activation:second", "0", Activation{Policy: p2, Enabled: true})
	chosen, reason, e = s.Select(ctx, admin, "one", issue)
	if e != nil || reason != "" || chosen.Ref != p2 {
		t.Fatal(chosen, reason, e)
	}
	policy.Name = "third"
	p3 := put("policy:third", "0", policy)
	active3 := put("activation:third", "0", Activation{Policy: p3, Enabled: true})
	_, reason, e = s.Select(ctx, admin, "one", issue)
	if e != nil || reason != "policy_conflict" {
		t.Fatal(reason, e)
	}
	put(active3.ID, active3.Version, Activation{Policy: p3, Enabled: false})
	put(active2.ID, active2.Version, Activation{Policy: p2, Enabled: false})
	// Explicit past application is replayable, bounded, and creates one key.
	past := ApplyPast{Mutation: mutation("0"), Policy: p, Limit: 1}
	r, e := s.ApplyPast(ctx, admin, "one", past)
	if e != nil || r.Data == nil {
		t.Fatal(r, e)
	}
	replayed, e := s.ApplyPast(ctx, admin, "one", past)
	if e != nil || replayed.Data == nil || *replayed.Data != *r.Data {
		t.Fatal("past replay", replayed, e)
	}
	job, e = s.Claim(ctx, "triage", "worker-one")
	if e != nil || job == nil {
		t.Fatal(job, e)
	}
	controlValue.Enabled = false
	ctl = put(ctl.ID, ctl.Version, controlValue)
	value = "P3"
	if e = s.Finish(ctx, *job, Result{Priority: &value, Reason: "must not apply", UsageState: "not_applicable"}); e != nil {
		t.Fatal(e)
	}
	status, e = s.Priority(ctx, admin, "one", issue.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = decode(status.Value, &priority)
	if *priority.Adopted != "P0" {
		t.Fatal("revoked authority overwrote priority")
	}
	controlValue.Enabled = true
	ctl = put(ctl.ID, ctl.Version, controlValue)
	action := JobAction{Mutation: mutation("0"), Action: "resume", Reason: "restored authority"}
	actionResult, actionErr := s.JobAction(ctx, admin, "one", job.ID, action)
	if actionErr != nil || actionResult.Data == nil {
		t.Fatal(actionResult, actionErr)
	}
	calls := 0
	worked, e := s.RunOne(ctx, "triage", "worker-restarted", func(context.Context, Job) (Result, error) {
		calls++
		return Result{}, errors.New("saved result must resume")
	})
	if e != nil || !worked || calls != 0 {
		t.Fatal("checkpoint/result recovery repeated I/O", worked, calls, e)
	}
	// Rights and output boundaries share current policy, including MCP none.
	if _, e = s.Put(ctx, access.Principal{UserID: "viewer"}, "one", "policy:denied", Write{Mutation: mutation("0"), Value: encode(policy)}); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("viewer write", e)
	}
	if _, e = s.Put(ctx, editor, "one", ctl.ID, Write{Mutation: mutation(ctl.Version), Value: encode(controlValue)}); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("editor control", e)
	}
	_, e = a.CreateOperation(ctx, admin, "one", "none", "read", true, nil, mutation("0"))
	if e != nil {
		t.Fatal(e)
	}
	none := admin
	none.OperationID = "none"
	page, e := s.Query(ctx, none, "one", Request{Mode: "records", Kind: "policy", Limit: 1})
	if e != nil || page.Data == nil || len(page.Data.Items) != 0 {
		t.Fatal("none broadened", page, e)
	}
	if _, e = s.Query(ctx, access.Principal{UserID: "outsider"}, "one", Request{Mode: "jobs"}); !errors.Is(e, access.ErrForbidden) {
		t.Fatal("workspace leak", e)
	}
	// Lease expiration returns the same request to one new owner and rejects old.
	issue = scope("issue", issue.Version, "milestone")
	lease, e := s.Claim(ctx, "triage", "lost-worker")
	if e != nil || lease == nil {
		t.Fatal(lease, e)
	}
	exec(`UPDATE automation_jobs SET lease_until=now()-interval '1 second' WHERE workspace_id='one' AND id=$1`, lease.ID)
	s = New(store, a)
	recovered, e := s.Claim(ctx, "triage", "new-worker")
	if e != nil || recovered == nil || recovered.ID != lease.ID || recovered.Lease == lease.Lease {
		t.Fatal(recovered, e)
	}
	if e = s.Checkpoint(ctx, *lease, encode("old")); !errors.Is(e, ErrLease) {
		t.Fatal("old lease accepted", e)
	}
	if e = s.retry(ctx, *recovered, "fixture failure"); e != nil {
		t.Fatal(e)
	}
	t.Run("parallel_claim", func(t *testing.T) {
		var wg sync.WaitGroup
		claims := make(chan *Job, 2)
		for _, name := range []string{"a", "b"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				j, e := s.Claim(ctx, "triage", name)
				if e != nil {
					t.Error(e)
				}
				claims <- j
			}()
		}
		wg.Wait()
		close(claims)
		count := 0
		for j := range claims {
			if j != nil {
				count++
				if r, e := s.JobAction(ctx, admin, "one", j.ID, JobAction{Mutation: mutation("0"), Action: "stop", Reason: "end claim fixture"}); e != nil || r.Data == nil {
					t.Fatal(r, e)
				}
			}
		}
		if count != 1 {
			t.Fatal("expected one owner", count)
		}
	})
	t.Run("result_acceptance_and_separate_workers", func(t *testing.T) {
		exec(`CREATE TABLE fixture_accepted_results(id text PRIMARY KEY,version text NOT NULL,payload jsonb NOT NULL)`)
		var definition Ref
		definitionWrite, e := store.Apply(ctx, persistenceScope(admin, "one", func(pgx.Tx) error { return nil }), persistenceCommand("one", "fixture-definition:hopper", "fixture-definition", mutation("0"), encode("fixed definition")), func(tx pgx.Tx, r apicontract.Reference) (json.RawMessage, error) {
			definition = Ref{ID: r.ID, Version: r.Version}
			if e := a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: r.ID}, nil); e != nil {
				return nil, e
			}
			return encode("fixed definition"), nil
		})
		if e != nil || definitionWrite.Data == nil {
			t.Fatal(definitionWrite, e)
		}
		acceptResult := func(id, stop string) (Ref, error) {
			result, e := store.Apply(ctx, persistenceScope(admin, "one", func(pgx.Tx) error { return nil }), persistenceCommand("one", "fixture-result:"+id, "fixture-accepted-result", mutation("0"), encode(id)), func(tx pgx.Tx, r apicontract.Reference) (json.RawMessage, error) {
				if stop == "before-source" {
					return nil, errors.New("fixture stop before source")
				}
				if _, e := tx.Exec(ctx, `INSERT INTO fixture_accepted_results VALUES($1,$2,$3)`, r.ID, r.Version, encode(id)); e != nil {
					return nil, e
				}
				if stop == "after-source" {
					return nil, errors.New("fixture stop after source")
				}
				if e := a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: r.ID}, nil); e != nil {
					return nil, e
				}
				if e := s.AcceptResultTx(ctx, tx, "one", Source{Event: r.ID + "@" + r.Version, Target: Ref{ID: r.ID, Version: r.Version}}, definition); e != nil {
					return nil, e
				}
				if stop == "after-queue" {
					return nil, errors.New("fixture stop after queue")
				}
				return encode(id), nil
			})
			if e != nil {
				return Ref{}, e
			}
			if result.Data == nil {
				return Ref{}, ErrInvalid
			}
			return Ref{ID: result.Data.ID, Version: result.Data.Version}, nil
		}
		for _, point := range []string{"before-source", "after-source", "after-queue"} {
			if _, e = acceptResult(point, point); e == nil {
				t.Fatal("stop not injected", point)
			}
			var sources, jobs int
			_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM fixture_accepted_results WHERE id=$1", "fixture-result:"+point).Scan(&sources)
			_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs WHERE target_id=$1", "fixture-result:"+point).Scan(&jobs)
			if sources != 0 || jobs != 0 {
				t.Fatal("source/outbox split", point, sources, jobs)
			}
		}
		resultRef, e := acceptResult("accepted", "")
		if e != nil {
			t.Fatal(e)
		}
		var jobs int
		_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs WHERE target_id=$1", resultRef.ID).Scan(&jobs)
		if jobs != 1 {
			t.Fatal("accepted result missing queue")
		}
		// An observed completion is not an accepted result and never calls the
		// result contract, regardless of its text or completion flag.
		before := jobs
		_, e = store.Apply(ctx, persistenceScope(admin, "one", func(pgx.Tx) error { return nil }), persistenceCommand("one", "fixture-observed", "sync-report", mutation("0"), encode("completion_observed")), func(pgx.Tx, apicontract.Reference) (json.RawMessage, error) {
			return encode(contexts.Record{Ref: contexts.Ref{Kind: "report", ID: "observed", Revision: "observed-v1"}, Value: encode(map[string]any{"completion_observed": true, "result_state": "unconfirmed"})}), nil
		})
		if e != nil {
			t.Fatal(e)
		}
		_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM automation_jobs WHERE kind='hopper'").Scan(&jobs)
		if before != jobs {
			t.Fatal("observations promoted to accepted results")
		}
		hopperControl := controlValue
		hopperControl.Kind = "hopper"
		put("control:hopper", "0", hopperControl)
		for i := 0; i < 100; i++ {
			scope(fmt.Sprintf("queued-%03d", i), "0", "milestone")
		}
		oneConnection, err := persistence.Open(ctx, url+"&pool_max_conns=1")
		if err != nil {
			t.Fatal(err)
		}
		defer oneConnection.Close()
		a2 := access.New(oneConnection, []byte(strings.Repeat("k", 32)), nil)
		worker := New(oneConnection, a2)
		started := make(chan struct{})
		release := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			_, e := worker.RunOne(ctx, "triage", "slow-triage", func(run context.Context, j Job) (Result, error) {
				close(started)
				select {
				case <-release:
					return Result{Priority: &value, Reason: "external fixture", UsageState: "not_applicable"}, nil
				case <-run.Done():
					return Result{}, run.Err()
				}
			})
			finished <- e
		}()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("triage executor not started")
		}
		probe, cancel := context.WithTimeout(ctx, time.Second)
		var one int
		err = oneConnection.Database().QueryRow(probe, "SELECT 1").Scan(&one)
		cancel()
		if err != nil || one != 1 {
			close(release)
			t.Fatal("external I/O retained connection/transaction", err)
		}
		worked, e := worker.RunOne(ctx, "hopper", "independent-hopper", func(context.Context, Job) (Result, error) {
			return Result{Reason: "hopper fixture", UsageState: "not_applicable"}, nil
		})
		close(release)
		if e != nil || !worked {
			t.Fatal("triage starved hopper", worked, e)
		}
		if e = <-finished; e != nil {
			t.Fatal(e)
		}
		// Request-budget exhaustion is local to this kind and leaves history.
		hopperControl.MaxRequests = 1
		controlRecord, e := worker.Read(ctx, admin, "one", Ref{ID: "control:hopper", Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		put(controlRecord.ID, controlRecord.Version, hopperControl)
		ref, e := acceptResult("budgeted", "")
		if e != nil {
			t.Fatal(e)
		}
		next, e := worker.Claim(ctx, "hopper", "budget-worker")
		if e != nil || next != nil {
			t.Fatal(next, e)
		}
		var state, reason string
		_ = store.Database().QueryRow(ctx, "SELECT state,reason FROM automation_jobs WHERE target_id=$1", ref.ID).Scan(&state, &reason)
		if state != "blocked" || reason != "request_budget" {
			t.Fatal(state, reason)
		}
		// Monetary budgets use #14's fixed evaluations and one valuation/currency.
		hopperControl.MaxRequests = 1000
		controlRecord, e = worker.Read(ctx, admin, "one", Ref{ID: "control:hopper", Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		put(controlRecord.ID, controlRecord.Version, hopperControl)
		u := usage.New(store, a)
		uput := func(id string, v any) usage.Ref {
			t.Helper()
			r, e := u.Put(ctx, admin, "one", id, usage.Write{Mutation: mutation("0"), Value: encode(v)})
			if e != nil || r.Data == nil {
				t.Fatal(r, e)
			}
			return usage.Ref{ID: r.Data.ID, Version: r.Data.Version}
		}
		model, quantity := "fixture-model", "200"
		observation := usage.Observation{Source: "worker-fixture", Transform: "1", Event: "cost", Series: "cost", Mode: "delta", Group: "worker-cost", Request: "worker-cost", Coverage: "confirmed", Connection: "fixture", ActualModel: &model, At: time.Now().UTC(), Attribution: usage.Attribution{Kind: "hopper", ID: "fixture-evaluation", Scopes: []string{}}, Quantities: map[string]usage.Quantity{"input": {Value: &quantity, State: "reported", Unit: "token"}}}
		uput(usage.ObservationID(observation), observation)
		price := uput("price:worker-fixture", usage.Price{Connection: "fixture", Model: model, Currency: "USD", From: observation.At.Add(-time.Hour), Source: "fictional fixture", Scale: 6, Rounding: "half_up", Rates: map[string]usage.Rate{"input": {Amount: "0.001", Per: "1", Unit: "token"}}})
		evaluation := uput("evaluation:worker-fixture", usage.Evaluation{Group: observation.Group, Kind: "estimated", Price: &price})
		if _, e = acceptResult("charged", ""); e != nil {
			t.Fatal(e)
		}
		worked, e = worker.RunOne(ctx, "hopper", "charged-worker", func(context.Context, Job) (Result, error) {
			return Result{Reason: "fictional cost fixture", UsageState: "complete", Usage: []Ref{{ID: evaluation.ID, Version: evaluation.Version}}}, nil
		})
		if e != nil || !worked {
			t.Fatal(worked, e)
		}
		hopperControl.Budget = &Budget{Amount: "0.100000", Currency: "USD", Valuation: "estimated"}
		controlRecord, e = worker.Read(ctx, admin, "one", Ref{ID: "control:hopper", Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		put(controlRecord.ID, controlRecord.Version, hopperControl)
		ref, e = acceptResult("cost-limited", "")
		if e != nil {
			t.Fatal(e)
		}
		next, e = worker.Claim(ctx, "hopper", "cost-worker")
		if e != nil || next != nil {
			t.Fatal(next, e)
		}
		_ = store.Database().QueryRow(ctx, "SELECT state,reason FROM automation_jobs WHERE target_id=$1", ref.ID).Scan(&state, &reason)
		if state != "blocked" || reason != "cost_budget" {
			t.Fatal(state, reason)
		}
		hopperControl.Budget.Amount = "1000000"
		controlRecord, e = worker.Read(ctx, admin, "one", Ref{ID: "control:hopper", Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		put(controlRecord.ID, controlRecord.Version, hopperControl)
		if _, e = acceptResult("unknown-cost", ""); e != nil {
			t.Fatal(e)
		}
		worked, e = worker.RunOne(ctx, "hopper", "unknown-cost-worker", func(context.Context, Job) (Result, error) {
			return Result{Reason: "not reported", UsageState: "missing"}, nil
		})
		if e != nil || !worked {
			t.Fatal(worked, e)
		}
		ref, e = acceptResult("wait-for-usage", "")
		if e != nil {
			t.Fatal(e)
		}
		next, e = worker.Claim(ctx, "hopper", "uncertain-worker")
		if e != nil || next != nil {
			t.Fatal(next, e)
		}
		_ = store.Database().QueryRow(ctx, "SELECT state,reason FROM automation_jobs WHERE target_id=$1", ref.ID).Scan(&state, &reason)
		if state != "blocked" || reason != "usage_missing" {
			t.Fatal(state, reason)
		}
	})
	_ = parent
	_ = time.UTC
}
