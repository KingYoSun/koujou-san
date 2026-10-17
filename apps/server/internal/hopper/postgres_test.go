package hopper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type ownedPublisher struct {
	mu    sync.Mutex
	posts int
	lost  bool
	issue *publications.Issue
}

func (f *ownedPublisher) Connection(w, id string) (publications.Connection, bool) {
	return publications.Connection{ID: "fixed", Workspace: "one", Repository: "fixture/repo", RepositoryID: 8, InstallationID: 7}, w == "one" && id == "fixed"
}
func (f *ownedPublisher) Create(_ context.Context, _ publications.Connection, d publications.Draft, body string) publications.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts++
	f.issue = &publications.Issue{ID: 100, Number: 1, URL: "https://github.com/fixture/repo/issues/1", State: "open", Title: d.Title, Body: body, UpdatedAt: time.Now().UTC()}
	if f.lost {
		return publications.Outcome{State: "unknown", Reason: "response_lost", At: time.Now().UTC()}
	}
	return publications.Outcome{State: "created", Issue: f.issue, At: time.Now().UTC()}
}
func (f *ownedPublisher) Observe(_ context.Context, _ publications.Connection, marker string, _ int64) publications.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issue != nil && strings.Contains(f.issue.Body, marker) {
		return publications.Outcome{State: "created", Issue: f.issue, At: time.Now().UTC()}
	}
	return publications.Outcome{State: "unknown", Reason: "not_found", At: time.Now().UTC()}
}

func TestHopperPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:hopper")
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
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider');INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','editor','editor',false),('one','viewer','viewer',false),('two','outsider','admin',true)`)
	a := access.New(store, []byte(strings.Repeat("k", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.Header.Get("X-Fixture-User")
		if id == "" {
			w.WriteHeader(401)
		}
		return id, id != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	auto := automation.New(store, a)
	rs := runs.New(store, a, c)
	wf := workflows.New(store, a, c, rs)
	backend := &ownedPublisher{}
	pub := publications.New(store, a, rs, backend)
	credential := os.Getenv("KOUJOU_TEST_GRAPH_PASSWORD")
	remote, err := graphindex.NewRemote(os.Getenv("KOUJOU_TEST_GRAPH_URL"), "root", credential, "koujou_hopper_test")
	if err != nil {
		t.Fatal(err)
	}
	g := graphindex.New(store, a, c, remote, graphindex.DefaultLimits(), []byte(strings.Repeat("g", 32)))
	defer g.Close()
	s := New(store, a, auto, rs, wf, pub, g)
	p := access.Principal{UserID: "admin"}
	serial := 0
	m := func(version string) apicontract.Mutation {
		serial++
		return structMutation(fmt.Sprint("test-", serial), version)
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		r, e := accepted(v, err)
		if e != nil {
			t.Fatalf("mutation %+v: %v", v, e)
		}
		return r
	}
	ctrl := automation.Control{Kind: "hopper", Enabled: true, CanRead: true, CanCommit: true, Slots: 4, MaxAttempts: 3, LeaseSeconds: 60, TimeoutSeconds: 30, RetrySeconds: 0, MaxRequests: 1000, PeriodSeconds: 3600}
	control := must(auto.Put(ctx, p, "one", "control:hopper", automation.Write{Mutation: m("0"), Value: raw(ctrl)}))
	template := must(wf.Put(ctx, p, "one", "run-definition:repair", workflows.Write{Mutation: m("0"), Value: raw(workflows.Definition{Schema: "run-definition-v1", Name: "repair", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed", Parameters: raw([]string{})}}}, Selection: &contexts.Selection{Mode: "none"}})}))
	draft := must(pub.Put(ctx, p, "one", "publication-draft:template", publications.Write{Mutation: m("0"), Value: raw(publications.Draft{Name: "problem", Connection: "fixed", Title: "fixed problem", Body: "fixed evidence", Sources: []Ref{}})}))
	define := func(id string, d Definition) Ref {
		t.Helper()
		pin := must(s.Put(ctx, p, "one", "hopper:"+id, Write{Mutation: m("0"), Value: raw(d)}))
		must(s.Put(ctx, p, "one", "hopper-active:"+id, Write{Mutation: m("0"), Value: raw(Activation{Definition: pin, Enabled: true})}))
		return pin
	}
	base := Template()
	base.Name = "CI failure"
	base.Targets = []Target{{"github", "nightly"}}
	base.Problem = nil
	base.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
	base.Action.Template = template
	var pin Ref
	target := githubobserve.Target{ID: "nightly", Kind: "actions", ExternalID: "1", Repository: "fixture/repo", RepositoryID: 8, InstallationID: 7}
	exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES('nightly',$1,7,8)`, raw(target))
	exec(`INSERT INTO github_bindings VALUES('one','nightly','', '[]');INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one','github:nightly',false)`)
	pin = define("nightly", base)
	observed := func(version, run int64, conclusion string) {
		t.Helper()
		err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, target, githubobserve.Snapshot{State: "completed", Conclusion: conclusion, RunID: run, Attempt: 1}, version)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	step := func() {
		t.Helper()
		job, err := auto.Claim(ctx, "hopper", "owned-worker")
		if err != nil || job == nil {
			t.Fatal("claim", job, err)
		}
		result, err := s.Execute(ctx, *job)
		if err != nil {
			t.Fatal("execute", err)
		}
		if err = auto.Finish(ctx, *job, result); err != nil {
			t.Fatal("finish", err)
		}
	}
	evaluations := func() []Evaluation {
		t.Helper()
		page, err := s.Query(ctx, p, "one", Request{Mode: "heads", Kind: "evaluation", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := []Evaluation{}
		for _, r := range page.Items {
			var e Evaluation
			_ = json.Unmarshal(r.Value, &e)
			out = append(out, e)
		}
		return out
	}
	complete := func(id, state string, reports ...json.RawMessage) Ref {
		t.Helper()
		old, err := rs.Read(ctx, p, "one", Ref{ID: id, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var run runs.Run
		_ = json.Unmarshal(old.Value, &run)
		run.State = state
		now := time.Now().UTC()
		run.FinishedAt = &now
		if len(reports) > 0 {
			run.Assessment = &runs.Assessment{State: "valid", Verdict: "FAIL", Report: reports[0]}
		}
		if run.StartedAt == nil {
			run.StartedAt = &now
		}
		v, err := store.Apply(ctx, persistence.Scope{ActorID: "owned-result", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: id, Kind: "run-report", Mutation: m(old.Version), Payload: raw(map[string]string{"type": "finished"})}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
			if _, err := tx.Exec(ctx, `INSERT INTO run_records SELECT $1,$2,$2,$3,'run',sequence+1,$4,$5 FROM shared_streams WHERE workspace_id=$1`, "one", id, ref.Version, raw(run), raw(old.Refs)); err != nil {
				return nil, err
			}
			return raw(runs.Record{Ref: Ref{ID: id, Version: ref.Version}, RunID: id, Kind: "run", Value: raw(run), Refs: old.Refs}), nil
		})
		return must(v, err)
	}
	finish := func(id string, reports ...json.RawMessage) Ref { return complete(id, "failed", reports...) }
	t.Run("client_absent_CI_to_queued_Run_and_replay", func(t *testing.T) {
		observed(1, 11, "failure")
		step()
		all := evaluations()
		if len(all) != 1 || all[0].State != "published" || all[0].Destination == nil {
			t.Fatal(all)
		}
		r, err := rs.Read(ctx, p, "one", *all[0].Destination)
		if err != nil {
			t.Fatal(err)
		}
		var run runs.Run
		_ = json.Unmarshal(r.Value, &run)
		if run.State != "queued" || run.ExecutionRequestedAt == nil || run.Assignment != nil {
			t.Fatal(run)
		}
		input, err := rs.Read(ctx, p, "one", run.Input)
		if err != nil {
			t.Fatal(err)
		}
		var spec runs.Spec
		_ = json.Unmarshal(input.Value, &spec)
		if spec.Manifest == nil || len(spec.Input.Context) > 0 && string(spec.Input.Context) != "null" {
			t.Fatal("none input", spec)
		}
		observed(1, 11, "failure")
		worked, err := s.Step(ctx, "duplicate")
		if err != nil || worked {
			t.Fatal("duplicate work", worked, err)
		}
		if len(evaluations()) != 1 {
			t.Fatal("duplicate evaluation")
		}
	})
	t.Run("unknown_and_false_are_not_publications", func(t *testing.T) {
		for i, value := range []string{"cancelled", "timed_out", "future_conclusion"} {
			observed(int64(2+i), int64(12+i), value)
			step()
		}
		all := evaluations()
		if len(all) != 4 || all[1].State != "not_matched" || all[2].State != "not_matched" || all[3].State != "unknown" {
			t.Fatal(all)
		}
	})
	t.Run("saved_definition_does_not_replay_history", func(t *testing.T) {
		before := len(evaluations())
		base.Mode = "proposal"
		pin = must(s.Put(ctx, p, "one", pin.ID, Write{Mutation: m(pin.Version), Value: raw(base)}))
		if worked, err := s.Step(ctx, "save-only"); worked || err != nil {
			t.Fatal(worked, err)
		}
		active, err := s.Read(ctx, p, "one", Ref{ID: "hopper-active:nightly", Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		must(s.Put(ctx, p, "one", active.ID, Write{Mutation: m(active.Version), Value: raw(Activation{Definition: pin, Enabled: true})}))
		req := Past{Mutation: m("0"), Definition: pin, Kind: "github", Target: "*", Limit: 100}
		past := must(s.Past(ctx, p, "one", req))
		must(s.Past(ctx, p, "one", req))
		if len(evaluations()) != before {
			t.Fatal("historical trials became live heads")
		}
		r, err := s.Read(ctx, p, "one", past)
		if err != nil || !strings.Contains(string(r.Value), "hopper-trial:") {
			t.Fatal(r, err)
		}
		var summary struct {
			Items []Ref `json:"items"`
		}
		_ = json.Unmarshal(r.Value, &summary)
		seen := map[string]bool{}
		after := int64(0)
		afterID := ""
		for range 100 {
			page, err := s.Query(ctx, p, "one", Request{Mode: "history", Kind: "evaluation", After: after, AfterID: afterID, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 0 {
				break
			}
			for _, r := range page.Items {
				seen[r.ID] = true
			}
			after, afterID = page.After, page.AfterID
		}
		for _, trial := range summary.Items {
			if !seen[trial.ID] {
				t.Fatal("same-position trial skipped during pagination", trial.ID)
			}
		}
	})
	t.Run("Issue_response_loss_uses_shared_reconciliation", func(t *testing.T) {
		d := base
		d.Mode = "auto"
		d.Targets = []Target{{"github", "issue-ci"}}
		d.Action = Action{Kind: "issue", Template: draft}
		d.Problem = &Problem{Repository: "fixture/repo", Target: "nightly", Rule: "failure", IdentityPath: []string{"conclusion"}, WindowSeconds: 86400, Mode: "link_open"}
		target.ID = "issue-ci"
		exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES('issue-ci',$1,7,8)`, raw(target))
		exec(`INSERT INTO github_bindings VALUES('one','issue-ci','', '[]');INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one','github:issue-ci',false)`)
		define("issue", d)
		observed(1, 21, "failure")
		step()
		all := evaluations()
		e := all[len(all)-1]
		if e.State != "published" {
			t.Fatal(e)
		}
		backend.lost = true
		if err := pub.Process(ctx, "one", e.Destination.ID); err != nil {
			t.Fatal(err)
		}
		if err := pub.Process(ctx, "one", e.Destination.ID); err != nil {
			t.Fatal(err)
		}
		if backend.posts != 1 {
			t.Fatal("duplicate external write", backend.posts)
		}
		request, err := pub.Read(ctx, p, "one", Ref{ID: e.Destination.ID, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var published publications.Request
		_ = json.Unmarshal(request.Value, &published)
		if published.External == nil {
			t.Fatal(published)
		}
		observed(2, 22, "failure")
		step()
		all = evaluations()
		if all[len(all)-1].State != "linked" || backend.posts != 1 {
			t.Fatal(all[len(all)-1], backend.posts)
		}
	})
	t.Run("current_authority_and_read_only_queries", func(t *testing.T) {
		if _, err := s.Read(ctx, access.Principal{UserID: "outsider"}, "one", pin); err == nil {
			t.Fatal("cross-workspace fixed read")
		}
		for _, actor := range []access.Principal{{UserID: "viewer"}, {UserID: "outsider"}} {
			if _, err := s.Put(ctx, actor, "one", "hopper:denied", Write{Mutation: m("0"), Value: raw(base)}); err == nil {
				t.Fatal("write accepted", actor)
			}
		}
		before := len(evaluations())
		for range 3 {
			if _, err := s.Query(ctx, access.Principal{UserID: "viewer"}, "one", Request{Mode: "heads", Limit: 100}); err != nil {
				t.Fatal(err)
			}
		}
		if len(evaluations()) != before {
			t.Fatal("read caused side effect")
		}
		mux := http.NewServeMux()
		s.Register(mux)
		server := httptest.NewServer(mux)
		defer server.Close()
		res, err := server.Client().Get(server.URL + "/hopper/workspaces/one/template")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatal(res.StatusCode)
		}
		_ = control
	})
	t.Run("parallel_problem_reservation_and_recurrence", func(t *testing.T) {
		for _, id := range []string{"parallel-a", "parallel-b"} {
			tgt := target
			tgt.ID = id
			exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,7,8)`, id, raw(tgt))
			exec(`INSERT INTO github_bindings VALUES('one',$1,'','[]')`, id)
			exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, "github:"+id)
		}
		d := base
		d.Mode = "auto"
		d.Targets = []Target{{"github", "parallel-a"}, {"github", "parallel-b"}}
		d.Action = Action{Kind: "issue", Template: draft}
		d.Problem = &Problem{Repository: "fixture/repo", Target: "nightly", Rule: "parallel", IdentityPath: []string{"conclusion"}, WindowSeconds: 86400, Mode: "link_open"}
		define("parallel", d)
		before := len(evaluations())
		for i, id := range []string{"parallel-a", "parallel-b"} {
			tgt := target
			tgt.ID = id
			err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
				return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: int64(31 + i), Attempt: 1}, 1)
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		errors := make(chan error, 2)
		for i := range 2 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				worked := false
				var err error
				for range 20 {
					worked, err = s.Step(ctx, fmt.Sprint("parallel-", i))
					if worked || err != nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
				if !worked && err == nil {
					err = fmt.Errorf("no work")
				}
				errors <- err
			}(i)
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		all := evaluations()
		got := map[string]int{}
		var published Evaluation
		for _, e := range all[before:] {
			got[e.State]++
			if e.State == "published" {
				published = e
			}
		}
		if got["published"] != 1 || got["linked"] != 1 {
			t.Fatal(got)
		}
		if err := pub.Process(ctx, "one", published.Destination.ID); err != nil {
			t.Fatal(err)
		}
		if err := pub.Process(ctx, "one", published.Destination.ID); err != nil {
			t.Fatal(err)
		}
		backend.mu.Lock()
		backend.issue.State = "closed"
		backend.mu.Unlock()
		head, err := pub.Read(ctx, p, "one", Ref{ID: published.Destination.ID, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pub.Refresh(ctx, p, "one", head.Ref); err != nil {
			t.Fatal(err)
		}
		tgt := target
		tgt.ID = "parallel-a"
		if err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 33, Attempt: 1}, 2)
		}); err != nil {
			t.Fatal(err)
		}
		step()
		all = evaluations()
		last := all[len(all)-1]
		if last.State != "proposal" || last.Reason != "recurrence" {
			t.Fatal(last)
		}
	})
	t.Run("committed_effect_survives_worker_loss_and_new_lease", func(t *testing.T) {
		tgt := target
		tgt.ID = "nightly"
		base.Mode = "auto"
		pin = must(s.Put(ctx, p, "one", pin.ID, Write{Mutation: m(pin.Version), Value: raw(base)}))
		active, _ := s.Read(ctx, p, "one", Ref{ID: "hopper-active:nightly", Version: "head"})
		must(s.Put(ctx, p, "one", active.ID, Write{Mutation: m(active.Version), Value: raw(Activation{Definition: pin, Enabled: true})}))
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 41, Attempt: 1}, 10)
		}); err != nil {
			t.Fatal(err)
		}
		job, err := auto.Claim(ctx, "hopper", "lost-worker")
		if err != nil || job == nil {
			t.Fatal(job, err)
		}
		first, err := s.Execute(ctx, *job)
		if err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE automation_jobs SET lease_until=now()-interval '1 second' WHERE workspace_id='one' AND id=$1`, job.ID)
		reclaimed, err := auto.Claim(ctx, "hopper", "reclaimed-worker")
		if err != nil || reclaimed == nil || reclaimed.ID != job.ID {
			t.Fatal(reclaimed, err)
		}
		second, err := s.Execute(ctx, *reclaimed)
		if err != nil || string(raw(first)) != string(raw(second)) {
			t.Fatal(first, second, err)
		}
		if err = auto.Finish(ctx, *reclaimed, second); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Execute(ctx, *job); err == nil {
			t.Fatal("stale worker accepted")
		}
	})
	t.Run("cycle_depth_stop_and_explicit_resume", func(t *testing.T) {
		d := Template()
		d.Name = "cycle"
		d.Problem = nil
		d.Targets = []Target{{"run", "*"}}
		d.Condition = Condition{Field: "run.state", Op: "eq", Value: raw("failed")}
		d.Action.Template = template
		d.IncludeDerived = true
		d.Bounds.Depth = 2
		define("cycle", d)

		source := must(rs.Create(ctx, p, "one", "run:cycle-start", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}}}))
		finish(source.ID)
		step()
		all := evaluations()
		first := all[len(all)-1]
		if first.State != "published" || first.Depth != 0 {
			t.Fatal(first)
		}
		chainID := "hopper-chain:" + first.Root
		stop := must(s.Decide(ctx, p, "one", chainID, Decision{Mutation: m("0"), Action: "stop"}))
		finish(first.Destination.ID)
		step()
		all = evaluations()
		blockedRecord, err := s.Query(ctx, p, "one", Request{Mode: "heads", Kind: "evaluation", Root: first.Root, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		blocked := blockedRecord.Items[len(blockedRecord.Items)-1]
		var held Evaluation
		_ = json.Unmarshal(blocked.Value, &held)
		if held.State != "blocked" {
			t.Fatal(held)
		}
		must(s.Decide(ctx, p, "one", chainID, Decision{Mutation: m(stop.Version), Action: "resume"}))
		approved := must(s.Decide(ctx, p, "one", blocked.ID, Decision{Mutation: m(blocked.Version), Action: "publish"}))
		record, err := s.Read(ctx, p, "one", approved)
		if err != nil {
			t.Fatal(err)
		}
		var resumed Evaluation
		_ = json.Unmarshal(record.Value, &resumed)
		if resumed.State != "published" || resumed.Root != first.Root || resumed.Depth != 1 {
			t.Fatal(resumed)
		}
		finish(resumed.Destination.ID)
		step()
		all = evaluations()
		last := all[len(all)-1]
		if last.State != "blocked" || last.Root != first.Root || last.Depth != 2 {
			t.Fatal(last)
		}
		var count int
		if err = store.Database().QueryRow(ctx, `SELECT publications FROM hopper_chains WHERE workspace_id='one' AND id=$1`, first.Root).Scan(&count); err != nil || count != 2 {
			t.Fatal(count, err)
		}
	})
	activeCycle, _ := s.Read(ctx, p, "one", Ref{ID: "hopper-active:cycle", Version: "head"})
	var stoppedCycle Activation
	_ = json.Unmarshal(activeCycle.Value, &stoppedCycle)
	stoppedCycle.Enabled = false
	must(s.Put(ctx, p, "one", activeCycle.ID, Write{Mutation: m(activeCycle.Version), Value: raw(stoppedCycle)}))
	t.Run("revocation_and_missing_budget_before_commit_hold_work", func(t *testing.T) {
		tgt := target
		tgt.ID = "nightly"
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 51, Attempt: 1}, 11)
		}); err != nil {
			t.Fatal(err)
		}
		job, err := auto.Claim(ctx, "hopper", "before-revocation")
		if err != nil || job == nil {
			t.Fatal(job, err)
		}
		ctrl.Budget = &automation.Budget{Amount: "0", Currency: "USD", Valuation: "estimated"}
		control = must(auto.Put(ctx, p, "one", control.ID, automation.Write{Mutation: m(control.Version), Value: raw(ctrl)}))
		before := len(evaluations())
		if _, err = s.Execute(ctx, *job); err == nil {
			t.Fatal("exhausted cost budget accepted")
		}
		if len(evaluations()) != before {
			t.Fatal("budget rejection published")
		}
		var state string
		if err = store.Database().QueryRow(ctx, `SELECT state FROM automation_jobs WHERE workspace_id='one' AND id=$1`, job.ID).Scan(&state); err != nil || state != "blocked" {
			t.Fatal(state, err)
		}
		ctrl.Budget = nil
		control = must(auto.Put(ctx, p, "one", control.ID, automation.Write{Mutation: m(control.Version), Value: raw(ctrl)}))
		if err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 52, Attempt: 1}, 12)
		}); err != nil {
			t.Fatal(err)
		}
		job, err = auto.Claim(ctx, "hopper", "before-permission-loss")
		if err != nil || job == nil {
			t.Fatal(job, err)
		}
		exec(`UPDATE access_members SET role='editor' WHERE workspace_id='one' AND user_id='admin'`)
		if _, err = s.Execute(ctx, *job); err == nil {
			t.Fatal("revoked administrator published")
		}
		exec(`UPDATE access_members SET role='admin' WHERE workspace_id='one' AND user_id='admin'`)
	})
	t.Run("private_basis_is_removed_from_pages", func(t *testing.T) {
		exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id='github:issue-ci'`)
		page, err := s.Query(ctx, access.Principal{UserID: "viewer"}, "one", Request{Mode: "history", Kind: "evaluation", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw(page)), "issue-ci") {
			t.Fatal("private basis leaked")
		}
		exec(`UPDATE access_resources SET restricted=false WHERE workspace_id='one' AND id='github:issue-ci'`)
	})
	t.Run("Issue_Workflow_and_two_Hoppers_keep_one_origin", func(t *testing.T) {
		tgt := target
		tgt.ID = "flow-ci"
		exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,7,8)`, tgt.ID, raw(tgt))
		exec(`INSERT INTO github_bindings VALUES('one',$1,'','[]')`, tgt.ID)
		exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, "github:"+tgt.ID)
		a := Template()
		a.Name = "CI to Issue"
		a.Problem = nil
		a.Targets = []Target{{"github", tgt.ID}}
		a.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
		a.Action = Action{Kind: "issue", Template: draft}
		a.Bounds.Depth = 2
		define("flow-issue", a)
		b := Template()
		b.Name = "Workflow result to Run"
		b.Problem = nil
		b.Targets = []Target{{"run", "*"}}
		b.Condition = Condition{Field: "run.state", Op: "eq", Value: raw("failed")}
		b.Action.Template = template
		b.IncludeDerived = true
		define("flow-run", b)
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 61, Attempt: 1}, 1)
		}); err != nil {
			t.Fatal(err)
		}
		step()
		all := evaluations()
		origin := all[len(all)-1]
		var issue Ref
		if err := store.Database().QueryRow(ctx, `SELECT l.resource_id,h.version FROM hopper_lineage l JOIN shared_versions h ON h.workspace_id=l.workspace_id AND h.resource_id=l.resource_id WHERE l.workspace_id='one' AND l.root=$1 AND l.resource_id LIKE 'scope:%'`, origin.Root).Scan(&issue.ID, &issue.Version); err != nil {
			t.Fatal(err)
		}
		startWorkflow := func(name string, retry bool) (Ref, Ref) {
			d := workflows.Definition{Schema: "workflow-definition-v1", Name: name, Budget: &workflows.Budget{Parallel: 1, MaxRuns: 4}, Nodes: []workflows.Node{{ID: "work", Definition: &template}}}
			if retry {
				d.Nodes[0].Retry = map[string]string{"failed": "work"}
			}
			def := must(wf.Put(ctx, p, "one", "workflow-definition:"+name, workflows.Write{Mutation: m("0"), Value: raw(d)}))
			v := must(wf.Verify(ctx, p, "one", workflows.Verify{Mutation: m("0"), Definition: def}))
			ad := must(wf.Adopt(ctx, p, "one", workflows.Adopt{Mutation: m("0"), Definition: def, Validation: v}))
			ex := must(wf.Start(ctx, p, "one", "workflow-execution:"+name, workflows.Start{Mutation: m("0"), Adoption: ad, Sources: []Ref{issue}}))
			return ex, Ref{ID: "workflow-task:" + name + ":work:1", Version: ex.Version}
		}
		ex, task := startWorkflow("from-issue", false)
		var root string
		var depth int
		if err := store.Database().QueryRow(ctx, `SELECT root,depth FROM hopper_lineage WHERE workspace_id='one' AND resource_id=$1`, ex.ID).Scan(&root, &depth); err != nil || root != origin.Root || depth != 1 {
			t.Fatal(root, depth, err)
		}
		source := must(rs.Create(ctx, p, "one", "run:from-issue-workflow", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}, Sources: []Ref{task}}}))
		finish(source.ID, raw(map[string]string{"implementation_history": "NEVER_INJECT"}))
		step()
		all = evaluations()
		next := all[len(all)-1]
		if next.State != "published" || next.Root != origin.Root || next.Depth != 1 {
			t.Fatal(next)
		}
		rr, err := rs.Read(ctx, p, "one", *next.Destination)
		if err != nil {
			t.Fatal(err)
		}
		var run runs.Run
		_ = json.Unmarshal(rr.Value, &run)
		input, err := rs.Read(ctx, p, "one", run.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(input.Value), "NEVER_INJECT") {
			t.Fatal("implementation history implicitly injected")
		}
		finish(next.Destination.ID)
		step()
		all = evaluations()
		if last := all[len(all)-1]; last.State != "blocked" || last.Root != origin.Root || last.Depth != 2 {
			t.Fatal(last)
		}
		retryExecution, retryTask := startWorkflow("retry-owned", true)
		retryRun := must(rs.Create(ctx, p, "one", "run:workflow-owned-retry", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}, Sources: []Ref{retryTask}}}))
		exec(`UPDATE workflow_tasks SET run_id=$2,state='running',payload=jsonb_set(jsonb_set(payload,'{run}',$3::jsonb),'{state}','"running"') WHERE workspace_id='one' AND execution_id=$1 AND node_id='work'`, retryExecution.ID, retryRun.ID, raw(retryRun))
		finish(retryRun.ID)
		step()
		all = evaluations()
		if last := all[len(all)-1]; last.State != "held" || last.Reason != "workflow_owns_retry" {
			t.Fatal(last)
		}
		if err = wf.Step(ctx, "one", retryExecution.ID); err != nil {
			t.Fatal(err)
		}
		if err = wf.Step(ctx, "one", retryExecution.ID); err != nil {
			t.Fatal(err)
		}
		var attempts int
		if err = store.Database().QueryRow(ctx, `SELECT count(*) FROM workflow_tasks WHERE workspace_id='one' AND execution_id=$1`, retryExecution.ID).Scan(&attempts); err != nil || attempts != 2 {
			t.Fatal("ordinary Workflow retry missing", attempts, err)
		}

		if err = g.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		graph, err := g.Query(ctx, p, "one", graphindex.Request{Root: graphRef(origin.Source.Target), History: true, Depth: 8, Limit: 100})
		if err != nil || graph.Data == nil {
			t.Fatal(graph, err)
		}
		inherited := false
		for _, item := range graph.Data.Items {
			if item.Edge != nil && item.Edge.From.Kind == "scope" && item.Edge.To.Kind == "workflow-execution" && item.Edge.Origin != nil && item.Edge.Origin.Kind == "hopper-evaluation" {
				inherited = true
			}
		}
		if !inherited {
			t.Fatal("fixed Issue/Workflow provenance missing", string(raw(graph)))
		}
		active, _ := s.Read(ctx, p, "one", Ref{ID: "hopper-active:flow-run", Version: "head"})
		var activation Activation
		_ = json.Unmarshal(active.Value, &activation)
		activation.Enabled = false
		must(s.Put(ctx, p, "one", active.ID, Write{Mutation: m(active.Version), Value: raw(activation)}))
	})
	t.Run("required_local_evidence_waits_and_cost_missing_is_not_zero", func(t *testing.T) {
		d := Template()
		d.Name = "required log"
		d.Problem = nil
		d.Targets = []Target{{"run", "run:required-log"}}
		d.Condition = Condition{Field: "run.state", Op: "eq", Value: raw("failed")}
		d.Action.Template = template
		d.Required = []contexts.Fixed{{URI: "runner-log://owned/run/1", Delivery: "reference", State: "unfetched"}}
		r := must(rs.Create(ctx, p, "one", "run:required-log", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}}}))
		define("required-log", d)
		finish(r.ID)
		step()
		all := evaluations()
		if e := all[len(all)-1]; e.State != "input_waiting" || e.Destination != nil {
			t.Fatal(e)
		}
		var root string
		var runID string
		if err := store.Database().QueryRow(ctx, `SELECT l.root,l.resource_id FROM hopper_lineage l JOIN run_records r ON r.workspace_id=l.workspace_id AND r.id=l.resource_id JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE l.workspace_id='one' AND l.resource_id LIKE 'run:%' AND r.payload->>'started_at' IS NOT NULL ORDER BY l.resource_id LIMIT 1`).Scan(&root, &runID); err != nil {
			t.Fatal(err)
		}
		var cost usage.CostState
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			var e error
			cost, e = s.chainCost(ctx, tx, p, "one", root, usage.CostBudget{Amount: "100", Currency: "USD", Valuation: "estimated"})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if cost.State != "unobserved" || cost.Amount != nil {
			t.Fatal("missing cost became zero", runID, cost)
		}
	})
	t.Run("Runner_reconnect_assigns_same_Run_with_repository_area", func(t *testing.T) {
		tgt := target
		tgt.ID = "dispatch-ci"
		exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,7,8)`, tgt.ID, raw(tgt))
		exec(`INSERT INTO github_bindings VALUES('one',$1,'','[]')`, tgt.ID)
		exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, "github:"+tgt.ID)
		tr := must(wf.Put(ctx, p, "one", "run-definition:dispatch", workflows.Write{Mutation: m("0"), Value: raw(workflows.Definition{Schema: "run-definition-v1", Name: "dispatch", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "dispatch-only"}}}})}))
		d := Template()
		d.Name = "automatic assignment"
		d.Problem = nil
		d.Targets = []Target{{"github", tgt.ID}}
		d.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
		d.Action.Template = tr
		d.Action.Repository = &runs.Repository{ID: "repo"}
		d.Action.Mappings = []Mapping{{Path: []string{"commit"}, To: "repository.commit"}}
		define("dispatch", d)
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 71, Attempt: 1, Commit: strings.Repeat("a", 40)}, 1)
		}); err != nil {
			t.Fatal(err)
		}
		step()
		all := evaluations()
		e := all[len(all)-1]
		if e.State != "published" || e.Destination == nil {
			t.Fatal("dispatch publication", e.State, e.Reason)
		}
		id := e.Destination.ID
		for range 20 {
			worked, err := s.Dispatch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				break
			}
		}
		r, err := rs.Read(ctx, p, "one", Ref{ID: id, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var run runs.Run
		_ = json.Unmarshal(r.Value, &run)
		if run.State != "queued" || run.Assignment != nil {
			t.Fatal(run)
		}
		exec(`INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('hopper-device','one','admin','owned','owned','generation',true,true)`)
		profile := runs.Runner{Ref: Ref{ID: "runner:hopper-device", Version: "profile-v1"}, DeviceID: "hopper-device", RunnerID: "owned", SeenAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute), Connections: []runs.Connection{{Kind: "cli", Target: "dispatch-only", Version: "fixture-v1", Environment: "windows", Subject: "owned", Confirmed: true, Users: []string{"admin"}, Repositories: []string{"repo"}}}}
		exec(`INSERT INTO run_runners VALUES('one','hopper-device','profile-v1',$1)`, raw(profile))
		exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one','runner:hopper-device',false)`)
		exec(`UPDATE hopper_dispatch SET due_at=now() WHERE workspace_id='one' AND run_id=$1`, id)
		for range 20 {
			worked, err := s.Dispatch(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				break
			}
		}
		r, err = rs.Read(ctx, p, "one", Ref{ID: id, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(r.Value, &run)
		if run.Assignment == nil || run.Assignment.Repository == nil || run.Assignment.Repository.Commit != strings.Repeat("a", 40) || run.Assignment.Area == nil || !strings.HasPrefix(run.Assignment.Area.Branch, "koujou/run-") {
			t.Fatal(run)
		}
		var state string
		if err = store.Database().QueryRow(ctx, `SELECT state FROM hopper_dispatch WHERE workspace_id='one' AND run_id=$1`, id).Scan(&state); err != nil || state != "assigned" {
			t.Fatal(state, err)
		}
	})

	t.Run("execution_permission_is_separate_and_failed_publication_is_atomic", func(t *testing.T) {
		source := must(rs.Create(ctx, p, "one", "run:execute-denied", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}}}))
		d := Template()
		d.Name = "separate start permission"
		d.Problem = nil
		d.Targets = []Target{{"run", source.ID}}
		d.Condition = Condition{Field: "run.state", Op: "eq", Value: raw("failed")}
		d.Action.Template = template
		define("execute-denied", d)
		exec(`UPDATE access_members SET can_execute=false WHERE workspace_id='one' AND user_id='admin'`)
		var before, after int
		_ = store.Database().QueryRow(ctx, `SELECT count(*) FROM shared_versions WHERE workspace_id='one' AND resource_id LIKE 'run:hopper-%'`).Scan(&before)
		finish(source.ID)
		step()
		all := evaluations()
		last := all[len(all)-1]
		if last.State != "blocked" || last.Reason != "publication_authority_or_input_denied" || last.Destination != nil {
			t.Fatal(last.State, last.Reason)
		}
		_ = store.Database().QueryRow(ctx, `SELECT count(*) FROM shared_versions WHERE workspace_id='one' AND resource_id LIKE 'run:hopper-%'`).Scan(&after)
		if before != after {
			t.Fatal("partial Run escaped failed start authorization", before, after)
		}
		exec(`UPDATE access_members SET can_execute=true WHERE workspace_id='one' AND user_id='admin'`)
		page, err := s.Query(ctx, p, "one", Request{Mode: "heads", Kind: "evaluation", Root: last.Root, Limit: 100})
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		r := page.Items[0]
		approved := must(s.Decide(ctx, p, "one", r.ID, Decision{Mutation: m(r.Version), Action: "publish"}))
		current, err := s.Read(ctx, p, "one", approved)
		if err != nil {
			t.Fatal(err)
		}
		var e Evaluation
		_ = json.Unmarshal(current.Value, &e)
		if e.State != "published" || e.Destination == nil {
			t.Fatal(e.State, e.Reason)
		}
	})

	t.Run("delayed_Hopper_after_Workflow_retry_completed", func(t *testing.T) {
		d := workflows.Definition{Schema: "workflow-definition-v1", Name: "retry-delayed", Budget: &workflows.Budget{Parallel: 1, MaxRuns: 4}, Nodes: []workflows.Node{{ID: "work", Definition: &template, Retry: map[string]string{"failed": "work"}}}}
		def := must(wf.Put(ctx, p, "one", "workflow-definition:retry-delayed", workflows.Write{Mutation: m("0"), Value: raw(d)}))
		v := must(wf.Verify(ctx, p, "one", workflows.Verify{Mutation: m("0"), Definition: def}))
		ad := must(wf.Adopt(ctx, p, "one", workflows.Adopt{Mutation: m("0"), Definition: def, Validation: v}))
		ex := must(wf.Start(ctx, p, "one", "workflow-execution:retry-delayed", workflows.Start{Mutation: m("0"), Adoption: ad}))
		task := Ref{ID: "workflow-task:retry-delayed:work:1", Version: ex.Version}
		h := Template()
		h.Name = "delayed retry"
		h.Problem = nil
		h.Targets = []Target{{"run", "run:delayed-workflow-retry"}}
		h.Condition = Condition{Field: "run.state", Op: "eq", Value: raw("failed")}
		h.Action.Template = template
		r := must(rs.Create(ctx, p, "one", "run:delayed-workflow-retry", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed"}}, Sources: []Ref{task}}}))
		define("delayed-retry", h)
		exec(`UPDATE workflow_tasks SET run_id=$2,state='running',payload=jsonb_set(jsonb_set(payload,'{run}',$3::jsonb),'{state}','"running"') WHERE workspace_id='one' AND execution_id=$1 AND node_id='work'`, ex.ID, r.ID, raw(r))
		finish(r.ID)
		for range 3 {
			if err := wf.Step(ctx, "one", ex.ID); err != nil {
				t.Fatal(err)
			}
		}
		// The ordinary launch creates its Run and waits for the absent owned Runner.
		if err := wf.Step(ctx, "one", ex.ID); err != workflows.ErrState {
			t.Fatal("expected Runner wait", err)
		}
		var nextID string
		if err := store.Database().QueryRow(ctx, `SELECT run_id FROM workflow_tasks WHERE workspace_id='one' AND execution_id=$1 AND node_id='work' AND attempt=2`, ex.ID).Scan(&nextID); err != nil {
			t.Fatal(err)
		}
		complete(nextID, "succeeded")
		for range 2 {
			if err := wf.Step(ctx, "one", ex.ID); err != nil {
				t.Fatal(err)
			}
		}
		var state string
		if err := store.Database().QueryRow(ctx, `SELECT payload->>'state' FROM workflow_records WHERE workspace_id='one' AND id=$1 ORDER BY position DESC LIMIT 1`, ex.ID).Scan(&state); err != nil || state != "completed" {
			t.Fatal(state, err)
		}
		step()
		all := evaluations()
		if last := all[len(all)-1]; last.State != "held" || last.Reason != "workflow_owns_retry" || last.Destination != nil {
			t.Fatal("completed retry duplicated", last)
		}
	})
	t.Run("accepted_cycles_survive_Worker_delay", func(t *testing.T) {
		tgt := target
		tgt.ID = "delayed-cycles"
		exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,7,8)`, tgt.ID, raw(tgt))
		exec(`INSERT INTO github_bindings VALUES('one',$1,'','[]')`, tgt.ID)
		exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, "github:"+tgt.ID)
		d := Template()
		d.Name = "delayed cycles"
		d.Problem = nil
		d.Targets = []Target{{"github", tgt.ID}}
		d.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
		d.Action.Template = template
		define("delayed-cycles", d)
		accept := func(version int64) {
			if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
				return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 100 + version, Attempt: 1}, version)
			}); err != nil {
				t.Fatal(err)
			}
		}
		accept(1)
		accept(2)
		job, err := auto.Claim(ctx, "hopper", "restored-worker")
		if err != nil || job == nil || job.Event != "github:delayed-cycles:1" {
			t.Fatalf("first accepted cycle lost: %+v %v", job, err)
		}
		accept(3) // Also advance after Claim, before the commit-time authority check.
		result, err := s.Execute(ctx, *job)
		if err != nil {
			t.Fatal(err)
		}
		if err = auto.Finish(ctx, *job, result); err != nil {
			t.Fatal(err)
		}
		step()
		step()
		var count int
		if err = store.Database().QueryRow(ctx, `SELECT count(*) FROM hopper_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE r.workspace_id='one' AND r.kind='evaluation' AND r.payload->'source'->'target'->>'id'=$1 AND r.payload->>'state'='published'`, "hopper-result:"+tgt.ID).Scan(&count); err != nil || count != 3 {
			t.Fatal(count, err)
		}
	})
	t.Run("Run_problem_reservation_links_and_proposes_recurrence", func(t *testing.T) {
		for _, mode := range []string{"link_open", "hold", "recur"} {
			t.Run(mode, func(t *testing.T) {
				tgt := target
				tgt.ID = "run-problem-" + mode
				exec(`INSERT INTO github_targets(id,spec,installation_id,repository_id) VALUES($1,$2,7,8)`, tgt.ID, raw(tgt))
				exec(`INSERT INTO github_bindings VALUES('one',$1,'','[]')`, tgt.ID)
				exec(`INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, "github:"+tgt.ID)
				d := Template()
				d.Name = tgt.ID
				d.Targets = []Target{{"github", tgt.ID}}
				d.Action.Template = template
				d.Condition = Condition{Field: "ci.conclusion", Op: "eq", Value: raw("failure")}
				d.Problem = &Problem{Repository: "fixture/repo", Target: tgt.ID, Rule: "failure", IdentityPath: []string{"conclusion"}, WindowSeconds: 86400, Mode: mode}
				define(tgt.ID, d)
				accept := func(version int64) {
					if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
						return s.GitHubAccepted(ctx, tx, tgt, githubobserve.Snapshot{State: "completed", Conclusion: "failure", RunID: 200 + version, Attempt: 1}, version)
					}); err != nil {
						t.Fatal(err)
					}
				}
				accept(1)
				step()
				all := evaluations()
				first := all[len(all)-1]
				if first.State != "published" || first.Destination == nil {
					t.Fatal(first)
				}
				accept(2)
				job, err := auto.Claim(ctx, "hopper", "duplicate-run")
				if err != nil || job == nil {
					t.Fatal(job, err)
				}
				result, err := s.Execute(ctx, *job)
				if err != nil {
					_ = auto.Hold(ctx, *job, "test_failed")
					t.Fatal("second Run problem failed", err)
				}
				if err = auto.Finish(ctx, *job, result); err != nil {
					t.Fatal(err)
				}
				all = evaluations()
				second := all[len(all)-1]
				want := "linked"
				if mode == "hold" {
					want = "held"
				}
				if mode == "recur" {
					want = "published"
				}
				if second.State != want || second.Destination == nil || (mode != "recur" && *second.Destination != *first.Destination) {
					t.Fatal(second)
				}
				if mode == "link_open" {
					complete(first.Destination.ID, "succeeded")
					accept(3)
					step()
					all = evaluations()
					last := all[len(all)-1]
					if last.State != "proposal" || last.Reason != "recurrence" {
						t.Fatal(last)
					}
				}
			})
		}
	})
	t.Run("owned_HTTP_observer_to_Worker_without_client", func(t *testing.T) { checkObserverPipeline(t, s) })

}
