package hopper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/serverllm"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

func TestHopperSemanticPostgres(t *testing.T) {
	dsn := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("run pnpm run test:hopper")
	}
	ctx := context.Background()
	schema := "semantic_" + fmt.Sprintf("%x", time.Now().UnixNano())
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		_ = admin.Close(ctx)
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := persistence.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sql := func(query string, args ...any) {
		t.Helper()
		if _, err := store.Database().Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	sql(`INSERT INTO auth_users VALUES('admin','fixture','admin','admin@koujou.test','admin',false),('viewer','fixture','viewer','viewer@koujou.test','viewer',false);INSERT INTO access_workspaces VALUES('one','one','admin');INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','viewer','viewer',false)`)
	a := access.New(store, []byte(strings.Repeat("s", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		return r.Header.Get("X-Fixture-User"), r.Header.Get("X-Fixture-User") != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	auto := automation.New(store, a)
	rs := runs.New(store, a, c)
	wf := workflows.New(store, a, c, rs)
	pub := publications.New(store, a, rs, &ownedPublisher{})
	remote, err := graphindex.NewRemote(os.Getenv("KOUJOU_TEST_GRAPH_URL"), "root", os.Getenv("KOUJOU_TEST_GRAPH_PASSWORD"), "koujou_semantic_test")
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
		return structMutation(fmt.Sprint("semantic-test-", serial), version)
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		r, e := accepted(v, err)
		if e != nil {
			t.Fatalf("mutation %+v: %v", v, e)
		}
		return r
	}
	ctrl := automation.Control{Kind: "hopper", Enabled: true, CanRead: true, CanCommit: true, Slots: 2, MaxAttempts: 2, LeaseSeconds: 30, TimeoutSeconds: 20, RetrySeconds: 0, MaxRequests: 1000, PeriodSeconds: 3600}
	must(auto.Put(ctx, p, "one", "control:hopper", automation.Write{Mutation: m("0"), Value: raw(ctrl)}))
	template := must(wf.Put(ctx, p, "one", "run-definition:repair", workflows.Write{Mutation: m("0"), Value: raw(workflows.Definition{Schema: "run-definition-v1", Name: "repair", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "node", Parameters: raw([]string{"--version"}), Binding: map[string]string{"prompt": "stdin-json"}}}}, Selection: &contexts.Selection{Mode: "none"}})}))
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("x-bf-vk") != "owned-semantic-key" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("incorrect model credential separation")
		}
		var request struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
			Format   json.RawMessage                  `json:"response_format"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 || !strings.Contains(string(request.Format), "hopper_semantic_v1") {
			t.Error("invalid semantic request")
			w.WriteHeader(400)
			return
		}
		var evidence []SemanticEvidence
		_ = json.Unmarshal([]byte(request.Messages[1].Content), &evidence)
		var citation Citation
		for _, item := range evidence {
			if strings.Contains(string(item.Value), "DEFERRED_OWNED_BUG") {
				citation = Citation{Ref: item.Ref, Quote: "DEFERRED_OWNED_BUG"}
				break
			}
		}
		if citation.Ref.ID == "" {
			t.Error("real CLI output was not provided as fixed evidence")
		}
		decision := SemanticDecision{Verdict: "true", Reason: "unresolved issue was explicitly deferred", Candidate: &Finding{Identity: "owned-other-bug", Title: "Deferred owned bug", Body: "Investigate the separately deferred issue.", Status: "needs_reproduction"}, Evidence: []Citation{citation}, Unknown: []string{}}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-bifrost-request-id", r.Header.Get("x-request-id"))
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "owned-model-response", "model": "owned-model", "choices": []any{map[string]any{"message": map[string]any{"content": string(raw(decision))}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 23, "completion_tokens": 9, "prompt_tokens_details": map[string]int{"cached_tokens": 0}, "completion_tokens_details": map[string]int{"reasoning_tokens": 0}}, "owned_raw_marker": "preserved"})
	}))
	defer model.Close()
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "gateway-key")
	configFile := filepath.Join(dir, "connections.json")
	if err = os.WriteFile(keyFile, []byte("owned-semantic-key"), 0600); err != nil {
		t.Fatal(err)
	}
	config := []any{map[string]any{"id": "owned", "name": "Owned semantic gateway", "endpoint": model.URL + "/v1", "key_file": keyFile, "workspaces": []string{"one"}, "models": []serverllm.Model{{Requested: "openai/owned-model", Actual: []string{"owned-model"}}}}}
	if err = os.WriteFile(configFile, raw(config), 0600); err != nil {
		t.Fatal(err)
	}
	s.Gateway = &serverllm.Gateway{ConfigFile: configFile}
	connections, err := s.Gateway.Connections("one")
	if err != nil || len(connections) != 1 {
		t.Fatal(connections, err)
	}
	d := Template()
	d.Name = "Deferred issue in successful Run"
	d.Targets = []Target{{Kind: "run", ID: "*"}}
	d.Condition = Condition{All: []Condition{{Field: "run.state", Op: "eq", Value: raw("succeeded")}, {Semantic: true}}}
	d.Problem = nil
	d.Action.Template = template
	d.Action.Start = false
	d.Action.Mappings = []Mapping{{Path: []string{"semantic", "candidate"}, To: "prompt"}}
	sem := DefaultSemantic()
	sem.Connection = "owned"
	sem.Revision = connections[0].Revision
	sem.Model = "openai/owned-model"
	sem.Allowed = []string{"owned-model"}
	sem.Limits.Seconds = 5
	d.Semantic = &sem
	pin := must(s.Put(ctx, p, "one", "hopper:semantic", Write{Mutation: m("0"), Value: raw(d)}))
	must(s.Put(ctx, p, "one", "hopper-active:semantic", Write{Mutation: m("0"), Value: raw(Activation{Definition: pin, Enabled: true})}))
	origin := must(rs.Create(ctx, p, "one", "run:origin", runs.Create{Mutation: m("0"), Spec: runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "node", Parameters: raw([]string{"-e", "process.stdout.write('DEFERRED_OWNED_BUG: another problem was deferred as outside the agreed scope')"})}}}}))
	stdout, err := exec.CommandContext(ctx, "node", "-e", "process.stdout.write('DEFERRED_OWNED_BUG: another problem was deferred as outside the agreed scope')").Output()
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := rs.Read(ctx, p, "one", origin)
	if err != nil {
		t.Fatal(err)
	}
	var run runs.Run
	_ = json.Unmarshal(runRecord.Value, &run)
	now := time.Now().UTC()
	run.StartedAt = &now
	run.FinishedAt = &now
	run.State = "succeeded"
	zero := 0
	run.ExitCode = &zero
	run.ReportedThrough = "2"
	result := must(store.Apply(ctx, persistence.Scope{ActorID: "owned-cli-adapter", Grant: apicontract.Grant{WorkspaceID: "one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "one", ResourceID: origin.ID, Kind: "run-report", Mutation: m(origin.Version), Payload: raw(map[string]string{"type": "finished"})}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		outputID := "run-output:origin:1"
		output := runs.Output{Sequence: "1", Channel: "stdout", Source: "owned-real-node", Value: raw(string(stdout)), At: now}
		if _, err := tx.Exec(ctx, `INSERT INTO run_records SELECT $1,$2,$3,$4,'output',sequence+1,$5,$6 FROM shared_streams WHERE workspace_id=$1`, "one", origin.ID, outputID, ref.Version, raw(output), raw(runRecord.Refs)); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) VALUES('one',$1,false)`, outputID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO run_records SELECT $1,$2,$2,$3,'run',sequence+1,$4,$5 FROM shared_streams WHERE workspace_id=$1`, "one", origin.ID, ref.Version, raw(run), raw(runRecord.Refs)); err != nil {
			return nil, err
		}
		return raw(runs.Record{Ref: Ref{ID: origin.ID, Version: ref.Version}, RunID: origin.ID, Kind: "run", Value: raw(run), Refs: runRecord.Refs}), nil
	}))
	job, err := auto.Claim(ctx, "hopper", "owned-semantic-worker")
	if err != nil || job == nil {
		t.Fatal(job, err)
	}
	value, err := s.Execute(ctx, *job)
	if err != nil {
		t.Fatal("execute", err)
	}
	if err = auto.Finish(ctx, *job, value); err != nil {
		t.Fatal("finish", err)
	}
	page, err := s.Query(ctx, p, "one", Request{Mode: "heads", Kind: "evaluation", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	var evaluation Evaluation
	_ = json.Unmarshal(page.Items[0].Value, &evaluation)
	if evaluation.State != "published" || evaluation.Destination == nil || evaluation.Semantic == nil || evaluation.Semantic.Decision == nil {
		t.Fatalf("evaluation: %+v", evaluation)
	}
	before := calls.Load()
	if _, err = s.Execute(ctx, *job); err == nil {
		t.Fatal("finished lease reused")
	}
	if calls.Load() != before {
		t.Fatal("replay repeated model call")
	}
	original, err := rs.Read(ctx, p, "one", result)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(original.Value, &run)
	if run.State != "succeeded" {
		t.Fatal("source success was changed")
	}
	child, err := rs.Read(ctx, p, "one", *evaluation.Destination)
	if err != nil {
		t.Fatal(err)
	}
	var childRun runs.Run
	_ = json.Unmarshal(child.Value, &childRun)
	inputRecord, err := rs.Read(ctx, p, "one", childRun.Input)
	if err != nil {
		t.Fatal(err)
	}
	var spec runs.Spec
	_ = json.Unmarshal(inputRecord.Value, &spec)
	if !strings.Contains(string(spec.Input.Prompt), "needs_reproduction") || strings.Contains(string(spec.Input.Context), "DEFERRED_OWNED_BUG") {
		t.Fatal("finding or input separation was lost")
	}
	var count int
	var tokens string
	if err = store.Database().QueryRow(ctx, `SELECT count(*),max(payload->'quantities'->'input'->>'value') FROM usage_records u JOIN shared_versions h ON h.workspace_id=u.workspace_id AND h.resource_id=u.id AND h.version=u.version WHERE u.workspace_id='one' AND u.kind='usage' AND u.payload->'attribution'->>'kind'='hopper'`).Scan(&count, &tokens); err != nil || count != 1 || tokens != "23" {
		t.Fatal("usage", count, tokens, err)
	}
	t.Log("real owned Node output -> fixed model evidence -> server HTTP -> saved decision -> queued Run; original success, one model call and Hopper-only usage retained")
}
