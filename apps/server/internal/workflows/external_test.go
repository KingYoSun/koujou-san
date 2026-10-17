package workflows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

type fixtureObserver struct{ url string }

func (f fixtureObserver) Current(ctx context.Context, p access.Principal, w, id string) (githubobserve.Snapshot, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", f.url+"/"+id, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubobserve.Snapshot{}, err
	}
	defer res.Body.Close()
	var s githubobserve.Snapshot
	err = json.NewDecoder(res.Body).Decode(&s)
	return s, err
}
func externalScenario(t *testing.T, s *Service, device access.Principal, mutation func(string) apicontract.Mutation) {
	ctx := context.Background()
	p := access.Principal{UserID: "admin"}
	w := "one"
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		ref, err := accepted(v, err)
		if err != nil {
			t.Fatal(v, err)
		}
		return ref
	}
	put := func(id string, d Definition) Ref {
		return must(s.Put(ctx, p, w, id, Write{Mutation: mutation("0"), Value: raw(d)}))
	}
	adopt := func(def Ref) Ref {
		v := must(s.Verify(ctx, p, w, Verify{Mutation: mutation("0"), Definition: def}))
		return must(s.Adopt(ctx, p, w, Adopt{Mutation: mutation("0"), Definition: def, Validation: v}))
	}
	commit := strings.Repeat("c", 40)
	if _, err := s.Store.Database().Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one','github:fixed-ci',false),('one','github:fixed-pr',false)`); err != nil {
		t.Fatal(err)
	}
	snapshot := githubobserve.Snapshot{Commit: commit, RunID: 77, Attempt: 1, State: "completed", Conclusion: "success", Jobs: []githubobserve.Job{{Name: "required", Status: "completed", Conclusion: "success", Attempt: 1}}}
	reads, writes := 0, 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		reads++
		_ = json.NewEncoder(w).Encode(snapshot)
	}))
	defer remote.Close()
	s.Observer = fixtureObserver{remote.URL}
	ci := plainDefinition()
	ci.Spec.Input.Command = runs.Command{Kind: "observation", Target: "github"}
	ci.CI = &CICommand{Target: "fixed-ci", Commit: commit, RunID: "77", RequiredJobs: []string{"required"}}
	ciRef := put("run-definition:ci", ci)
	graph := Definition{Schema: "workflow-definition-v1", Name: "CI existing run", Budget: &Budget{Parallel: 1, MaxRuns: 1}, Nodes: []Node{{ID: "ci", Definition: &ciRef}}}
	ad := adopt(put("workflow-definition:ci", graph))
	root := must(s.Start(ctx, p, w, "workflow-execution:ci", Start{Mutation: mutation("0"), Adoption: ad}))
	for i := 0; i < 4; i++ {
		if err := s.Step(ctx, w, root.ID); err != nil {
			t.Fatal(err)
		}
	}
	ts, _ := tasks(ctx, s.Store.Database(), w, root.ID)
	if ts["ci"].Outcome != "PASS" || reads != 1 || writes != 0 {
		t.Fatal("existing CI observation triggered or misjudged", ts, reads, writes)
	}
	rr, err := s.Runs.Read(ctx, p, w, *ts["ci"].Run)
	if err != nil {
		t.Fatal(err)
	}
	var run runs.Run
	_ = json.Unmarshal(rr.Value, &run)
	if run.State != "succeeded" || run.Assessment.Verdict != "PASS" || run.Assignment != nil {
		t.Fatal("observer command and external result conflated", run)
	}
	snapshot.Commit = "changed"
	if ciVerdict(*ci.CI, snapshot) != "INCONCLUSIVE" {
		t.Fatal("old commit accepted")
	}
	snapshot.Commit = commit
	snapshot.Jobs[0].Conclusion = "skipped"
	if ciVerdict(*ci.CI, snapshot) != "INCONCLUSIVE" {
		t.Fatal("skip accepted")
	}
	snapshot.Jobs[0].Conclusion = "failure"
	if ciVerdict(*ci.CI, snapshot) != "FAIL" {
		t.Fatal("failed job accepted")
	}
	snapshot.Jobs[0].Conclusion = "success"
	write := plainDefinition()
	write.DeviceID = "worker"
	write.Spec.Input.Command.Parameters = raw(map[string]string{"sha": commit})
	write.Write = &WriteGuard{Target: "fixed-pr", Commit: commit, Required: []string{"gate"}, Conditional: true, Parameter: []string{"sha"}}
	wr := put("run-definition:merge", write)
	graph.Name = "explicit conditional operation"
	graph.Nodes = []Node{{ID: "gate", Choices: []string{"approve", "rework"}}, {ID: "merge", Definition: &wr, Join: "all", Depends: []Dependency{{Node: "gate", On: []string{"approve"}, Required: true}}}}
	ad = adopt(put("workflow-definition:merge", graph))
	root = must(s.Start(ctx, p, w, "workflow-execution:merge", Start{Mutation: mutation("0"), Adoption: ad}))
	step := func() {
		t.Helper()
		if err := s.Step(ctx, w, root.ID); err != nil {
			t.Fatal(err)
		}
	}
	step()
	ts, _ = tasks(ctx, s.Store.Database(), w, root.ID)
	if ts["merge"].Run != nil {
		t.Fatal("missing required judgment launched command")
	}
	_, head, _ := execution(ctx, s.Store.Database(), w, root.ID)
	must(s.Decide(ctx, access.Principal{UserID: "editor"}, w, root.ID, Decision{Mutation: mutation(head.Version), Node: "gate", Attempt: 1, Choice: "approve"}))
	step()
	snapshot.Commit = "changed"
	if err = s.Step(ctx, w, root.ID); err == nil || writes != 0 {
		t.Fatal("changed target permitted external write")
	}
	snapshot.Commit = commit
	step()
	ts, _ = tasks(ctx, s.Store.Database(), w, root.ID)
	rr, err = s.Runs.Read(ctx, p, w, Ref{ID: ts["merge"].Run.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(rr.Value, &run)
	if run.Assignment == nil {
		t.Fatal("conditional command was not assigned")
	}
	// This is the same assigned-input boundary the native Runner checks before
	// invoking the configured tool. The fake external endpoint also requires SHA.
	snapshot.Commit = "changed"
	if _, err = s.Runs.Assigned(ctx, device, w, rr.ID, run.Assignment.Generation); err == nil {
		t.Fatal("target changed after assignment")
	}
	snapshot.Commit = commit
	input, err := s.Runs.Assigned(ctx, device, w, rr.ID, run.Assignment.Generation)
	value, valueErr := selectValue(input.Spec.Input.Command.Parameters, []string{"sha"})
	if err != nil || valueErr != nil || string(value) != string(raw(commit)) {
		t.Fatal("conditional parameter lost", input, err)
	}
	response, err := http.Post(remote.URL+"/merge", "application/json", strings.NewReader(string(input.Spec.Input.Command.Parameters)))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if writes != 1 {
		t.Fatal("matching explicit command did not write once")
	}
	t.Log("fixed HTTP CI observation made no CI-start/write request; target/version/required judgments/current permission and conditional command survive the native start boundary")
	// Explicit budget values control additional starts. No missing consumption is
	// silently converted to zero, and alternatives/currencies are never summed.
	budget := CostBudget{Amount: "1.00", Currency: "USD", Valuation: "reported"}
	missing, err := s.cost(ctx, s.Store.Database(), p, w, "workflow-execution:fixed", budget)
	if err != nil || missing.State != "unobserved" || missing.Amount != nil {
		t.Fatal("missing cost counted as zero", missing, err)
	}
	_, err = s.Store.Database().Exec(ctx, `INSERT INTO usage_entries SELECT t.workspace_id,'workflow-budget-'||t.run_id,1,'budget-'||t.run_id,'reported','USD',now(),'run',t.run_id,t.execution_id,'{}','fixture',0.10,'rated',jsonb_build_array(jsonb_build_object('id',t.run_id,'version',v.version)),'{}','{}' FROM workflow_tasks t JOIN shared_versions v ON v.workspace_id=t.workspace_id AND v.resource_id=t.run_id WHERE t.workspace_id=$1 AND t.execution_id='workflow-execution:fixed'`, w)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := s.cost(ctx, s.Store.Database(), p, w, "workflow-execution:fixed", budget)
	if err != nil || observed.State != "observed" || observed.Amount == nil || *observed.Amount != "0.80" {
		t.Fatal("exact observed cost", observed, err)
	}
	budget.Amount = "0.80"
	exhausted, err := s.cost(ctx, s.Store.Database(), p, w, "workflow-execution:fixed", budget)
	if err != nil || exhausted.State != "exhausted" {
		t.Fatal("cost budget not enforced", exhausted, err)
	}
	_, err = s.Store.Database().Exec(ctx, `UPDATE usage_entries SET currency='JPY' WHERE workspace_id=$1 AND projection_key LIKE 'workflow-budget-%'`, w)
	if err != nil {
		t.Fatal(err)
	}
	otherCurrency, err := s.cost(ctx, s.Store.Database(), p, w, "workflow-execution:fixed", budget)
	if err != nil || otherCurrency.Amount != nil || otherCurrency.State != "unobserved" {
		t.Fatal("currency mixed", otherCurrency, err)
	}
	graph.Name = "finite retry budget"
	graph.Budget = &Budget{Parallel: 1, MaxRuns: 1}
	graph.Nodes = []Node{{ID: "ci", Definition: &ciRef, Retry: map[string]string{"FAIL": "ci"}}}
	snapshot.Jobs[0].Conclusion = "failure"
	ad = adopt(put("workflow-definition:run-budget", graph))
	bounded := must(s.Start(ctx, p, w, "workflow-execution:run-budget", Start{Mutation: mutation("0"), Adoption: ad}))
	for i := 0; i < 4; i++ {
		if err = s.Step(ctx, w, bounded.ID); err != nil {
			t.Fatal(err)
		}
	}
	limited, _, err := execution(ctx, s.Store.Database(), w, bounded.ID)
	if err != nil || limited.State != "stopped" || limited.Count != 1 || limited.Reason != "run_budget" {
		t.Fatal("finite retry budget", limited, err)
	}
	graph.Name = "time budget"
	graph.Budget = &Budget{Parallel: 1, MaxRuns: 1, Seconds: 1}
	graph.Nodes = []Node{{ID: "command", Definition: &wr}}
	// A standalone explicit command still requires its configured judgment.
	plain := plainDefinition()
	plain.DeviceID = "worker"
	plainRef := put("run-definition:time-budget", plain)
	graph.Nodes[0].Definition = &plainRef
	ad = adopt(put("workflow-definition:time-budget", graph))
	timed := must(s.Start(ctx, p, w, "workflow-execution:time-budget", Start{Mutation: mutation("0"), Adoption: ad}))
	if err = s.Step(ctx, w, timed.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err = s.Step(ctx, w, timed.ID); err != nil {
		t.Fatal(err)
	}
	ended, _, err := execution(ctx, s.Store.Database(), w, timed.ID)
	if err != nil || ended.State != "stopped" || ended.Reason != "time_budget_no_further_start" {
		t.Fatal("time budget", ended, err)
	}
	ts, _ = tasks(ctx, s.Store.Database(), w, timed.ID)
	var created int
	_ = s.Store.Database().QueryRow(ctx, `SELECT count(*) FROM run_records WHERE workspace_id=$1 AND id=$2`, w, ts["command"].Run.ID).Scan(&created)
	if created != 0 {
		t.Fatal("expired prepared task created a Run")
	}
	t.Log("finite retry/time budgets, exact chosen cost valuation, missing coverage and currency boundaries passed")
}
