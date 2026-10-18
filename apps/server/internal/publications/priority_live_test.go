package publications

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

type priorityAllowance struct{ Path string }

func (b priorityAllowance) use(kind string) error {
	limits := map[string]int{"issue_creates": 1, "label_creates": 2, "label_writes": 4, "human_writes": 1, "issue_closes": 1, "label_deletes": 2}
	max, ok := limits[kind]
	if !ok || b.Path == "" {
		return errors.New("explicit priority trial allowance required")
	}
	lock, err := os.OpenFile(b.Path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close(); _ = os.Remove(b.Path + ".lock") }()
	data, err := os.ReadFile(b.Path)
	if err != nil {
		return err
	}
	var counts map[string]int
	if kind == "label_creates" || kind == "label_deletes" {
		// A continuation restores only the two labels on an already-created Issue.
		// The extra two creations/deletions require a separate, explicit allowance.
		var approved map[string]int
		if json.Unmarshal(data, &approved) == nil && approved["resume_issue"] > 0 && approved["extra_label_pairs"] > 0 && approved["extra_label_pairs"] <= 2 {
			max += 2 * approved["extra_label_pairs"]
		}
	}
	if json.Unmarshal(data, &counts) != nil || counts["approved"] != 1 || counts[kind] < 0 || counts[kind] >= max {
		return errors.New("priority trial allowance exhausted")
	}
	counts[kind]++
	return os.WriteFile(b.Path, raw(counts), 0600)
}

type approvedPriority struct {
	Remote *Remote
	Budget priorityAllowance
}

func TestPriorityAllowanceDoesNotExpand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowance.json")
	if err := os.WriteFile(path, raw(map[string]int{"approved": 1}), 0600); err != nil {
		t.Fatal(err)
	}
	allowance := priorityAllowance{Path: path}
	for kind, limit := range map[string]int{"issue_creates": 1, "label_creates": 2, "label_writes": 4, "human_writes": 1, "issue_closes": 1, "label_deletes": 2} {
		for i := 0; i < limit; i++ {
			if err := allowance.use(kind); err != nil {
				t.Fatal(kind, err)
			}
		}
		if err := allowance.use(kind); err == nil {
			t.Fatal("expanded trial", kind)
		}
	}
	if err := os.WriteFile(path, raw(map[string]int{"approved": 1, "resume_issue": 63, "extra_label_pairs": 1, "issue_creates": 1, "label_creates": 2, "label_deletes": 2}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"label_creates", "label_deletes"} {
		for i := 0; i < 2; i++ {
			if err := allowance.use(kind); err != nil {
				t.Fatal(err)
			}
		}
		if err := allowance.use(kind); err == nil {
			t.Fatal("continuation exceeded two restored labels", kind)
		}
	}
	if err := allowance.use("issue_creates"); err == nil {
		t.Fatal("continuation permitted a second Issue")
	}
}

func (b approvedPriority) ReadPriority(ctx context.Context, w, id string, n int64, page int) (automation.ExternalSnapshot, error) {
	return b.Remote.ReadPriority(ctx, w, id, n, page)
}
func (b approvedPriority) WritePriority(ctx context.Context, w, id string, n int64, action, label string) error {
	if err := b.Budget.use("label_writes"); err != nil {
		return err
	}
	return b.Remote.WritePriority(ctx, w, id, n, action, label)
}

// Opt-in only: one synthetic Issue, two uniquely named labels and the existing
// limited App. It never uses a production Workspace or calls a model.
func TestPriorityLive(t *testing.T) {
	configFile := os.Getenv("KOUJOU_TEST_PRIORITY_LIVE_CONFIG")
	if configFile == "" {
		t.Skip("explicit App, allowance and owned database required")
	}
	budget := priorityAllowance{Path: os.Getenv("KOUJOU_TEST_PRIORITY_LIVE_BUDGET")}
	if budget.Path == "" {
		t.Fatal("explicit allowance required")
	}
	t.Setenv("KOUJOU_PUBLICATION_CONFIG", configFile)
	cfg, err := Environment()
	if err != nil || cfg == nil || len(cfg.Connections) != 1 {
		t.Fatal("one explicit publisher App connection required")
	}
	connection := cfg.Connections[0]
	if connection.Repository != "KingYoSun/koujou-san" || connection.RepositoryID != 1418646340 {
		t.Fatal("trial repository not approved")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Fatal("owned database required")
	}
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w, user := connection.Workspace, "priority-live-user"
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO auth_users VALUES($1,'fixture',$1,'priority-live@koujou.test','priority live',false)`, []any{user}},
		{`INSERT INTO access_workspaces VALUES($1,'owned priority trial',$2)`, []any{w, user}},
		{`INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES($1,$2,'admin',true)`, []any{w, user}},
	} {
		if _, err = store.Database().Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	a := access.New(store, []byte(strings.Repeat("p", 32)), nil)
	auto := automation.New(store, a)
	remote := NewRemote(cfg)
	auto.PriorityBackend = approvedPriority{Remote: remote, Budget: budget}
	actor := access.Principal{UserID: user}
	token, err := remote.token(ctx, connection)
	if err != nil {
		t.Fatal("App exchange failed")
	}
	tag := "koujou-triage-" + strings.ToLower(rand.Text()[:8])
	labels := []string{tag + "-high", tag + "-normal"}
	created := []string{}
	var issue *Issue
	if path := os.Getenv("KOUJOU_TEST_PRIORITY_RESUME_REPORT"); path != "" {
		var previous struct {
			Repository string
			Number     int64 `json:"issue_number"`
			Labels     []string
			Closed     bool `json:"issue_closed"`
		}
		var allowance map[string]int
		data, readErr := os.ReadFile(path)
		approved, approvalErr := os.ReadFile(budget.Path)
		if readErr != nil || approvalErr != nil || json.Unmarshal(data, &previous) != nil || json.Unmarshal(approved, &allowance) != nil || previous.Repository != connection.Repository || len(previous.Labels) != 2 || !previous.Closed || previous.Number <= 0 || int64(allowance["resume_issue"]) != previous.Number {
			t.Fatal("explicit continuation allowance and original closed Issue report required")
		}
		observed := remote.Observe(ctx, connection, "", previous.Number)
		if observed.Issue == nil || observed.Issue.State != "closed" {
			t.Fatal("original Issue must remain closed")
		}
		issue, labels = observed.Issue, previous.Labels
	}
	report := map[string]any{"repository": connection.Repository, "labels": labels, "llm_requests": 0, "issue_closed": false, "labels_deleted": []string{}}
	defer func() {
		if path := os.Getenv("KOUJOU_TEST_PRIORITY_LIVE_REPORT"); path != "" {
			if err := os.WriteFile(path, raw(report), 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	request := func(method, path string, body any) error {
		var payload io.Reader
		if body != nil {
			payload = strings.NewReader(string(raw(body)))
		}
		response, err := remote.request(ctx, method, path, token, payload)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("owned GitHub request status %d", response.StatusCode)
		}
		return nil
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		ctx = cleanup
		if issue != nil && issue.State != "closed" {
			if err := budget.use("issue_closes"); err != nil {
				t.Error(err)
			} else if err = request("PATCH", "/repos/"+connection.Repository+"/issues/"+fmt.Sprint(issue.Number), map[string]string{"state": "closed"}); err != nil {
				t.Error("synthetic Issue cleanup failed", err)
			} else {
				report["issue_closed"] = true
			}
		} else if issue != nil {
			report["issue_closed"] = true
		}
		deleted := []string{}
		for _, label := range created {
			if err := budget.use("label_deletes"); err != nil {
				t.Error(err)
				continue
			}
			if err := request("DELETE", "/repos/"+connection.Repository+"/labels/"+url.PathEscape(label), nil); err != nil {
				t.Error("synthetic label cleanup failed", err)
			} else {
				deleted = append(deleted, label)
			}
		}
		report["labels_deleted"] = deleted
	}()
	for _, label := range labels {
		if err = budget.use("label_creates"); err != nil {
			t.Fatal(err)
		}
		if err = request("POST", "/repos/"+connection.Repository+"/labels", map[string]string{"name": label, "color": "6a737d", "description": "koujou-san #28 owned temporary priority trial"}); err != nil {
			t.Fatal(err)
		}
		created = append(created, label)
	}
	body := "koujou-san #28の合成接続試験です。優先度ラベルだけを同期します。実データや秘密値は含みません。"
	if issue == nil {
		if err = budget.use("issue_creates"); err != nil {
			t.Fatal(err)
		}
		outcome := remote.Create(ctx, connection, Draft{Title: "[koujou-san #28 接続試験] " + tag}, body+"\n\n"+marker(w, tag))
		issue = outcome.Issue
	}
	if issue == nil {
		t.Fatal("synthetic creation remains unconfirmed; do not create again")
	}
	report["issue_url"] = issue.URL
	report["issue_number"] = issue.Number
	n := 0
	mutation := func(version string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: fmt.Sprint("priority-live-", n), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) automation.Ref {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatal(v, err)
		}
		return automation.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	put := func(id string, value any) automation.Ref {
		return must(auto.Put(ctx, actor, w, id, automation.Write{Mutation: mutation("0"), Value: raw(value)}))
	}
	scope := must(auto.Contexts.Put(ctx, actor, w, contexts.Identity{Kind: "scope", ID: "priority-live"}, contexts.Write{Mutation: mutation("0"), Value: raw(contexts.Scope{Kind: "issue", Name: "owned priority trial"})}))
	policy := put("policy:priority-live", automation.Policy{Name: "owned priority trial", Scopes: []string{"priority-live"}, Levels: automation.DefaultLevels(), Criteria: "synthetic fixture", Context: []automation.Ref{}, External: &automation.ExternalPolicy{Connection: connection.ID, Labels: map[string]string{"P1": labels[0], "P2": labels[1]}}})
	put("activation:priority-live", automation.Activation{Policy: policy, Enabled: true, AutoApply: true})
	put("control:triage", automation.Control{Kind: "triage", Enabled: true, CanRead: true, CanCommit: true, Slots: 2, MaxAttempts: 1, LeaseSeconds: 30, TimeoutSeconds: 10, MaxRequests: 10, PeriodSeconds: 3600})
	put("issue:priority-live", automation.Issue{Scope: scope, Origin: "github", Title: issue.Title, Body: body, State: "open", Dependencies: []automation.Ref{}, External: &automation.ExternalPriority{Connection: connection.ID, Number: issue.Number}})
	p1 := "P1"
	put("priority:"+scope.ID, automation.PriorityEdit{Target: scope, Policy: policy, Value: &p1, Reason: "synthetic already exported manual priority"})
	resolvedHistory := false
	reconcile := func() {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := store.Database().Exec(ctx, `UPDATE triage_sync SET due_at=now()`); err != nil {
				t.Fatal(err)
			}
			if _, err := auto.SyncPriority(ctx); err != nil {
				t.Fatal(err)
			}
			rec, err := auto.Read(ctx, actor, w, automation.Ref{ID: "priority-sync:" + scope.ID, Version: "head"})
			if err != nil {
				continue
			}
			var state automation.PrioritySync
			_ = json.Unmarshal(rec.Value, &state)
			report["last_sync"] = state
			if os.Getenv("KOUJOU_TEST_PRIORITY_RESUME_REPORT") != "" && !resolvedHistory && state.State == "conflict" && state.Reason == "concurrent_manual_priority" {
				previous, err := auto.Read(ctx, actor, w, automation.Ref{ID: "priority:" + scope.ID, Version: "head"})
				if err != nil {
					t.Fatal(err)
				}
				must(auto.Put(ctx, actor, w, previous.ID, automation.Write{Mutation: mutation(previous.Version), Value: raw(automation.PriorityEdit{Target: scope, Policy: policy, Value: &p1, Reason: "explicit synthetic continuation after preserved historical conflict"})}))
				resolvedHistory = true
				report["historical_conflict_resolved"] = true
				continue
			}
			if state.State == "synced" {
				return
			}
		}
		t.Fatal("priority reconciliation did not converge")
	}
	reconcile()
	observed, err := remote.ReadPriority(ctx, w, connection.ID, issue.Number, 1)
	if err != nil || !slices.Contains(observed.Labels, labels[0]) {
		t.Fatal("App priority not observed", err)
	}
	own := false
	for _, event := range observed.Events {
		own = own || (event.Label == labels[0] && event.Action == "labeled" && event.Own)
	}
	if !own {
		t.Fatal("App write provenance not reported")
	}
	report["own_write_distinguished"] = true
	if err = budget.use("human_writes"); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(t.TempDir(), "human-labels.json")
	if err = os.WriteFile(payload, raw(map[string]any{"labels": []string{labels[1]}}), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "PUT", "repos/"+connection.Repository+"/issues/"+fmt.Sprint(issue.Number)+"/labels", "--input", payload)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = output
		t.Fatal("owned external human label change failed")
	}
	reconcile()
	rec, err := auto.Priority(ctx, actor, w, scope.ID)
	if err != nil {
		t.Fatal(err)
	}
	var priority automation.Priority
	_ = json.Unmarshal(rec.Value, &priority)
	if priority.Adopted == nil || *priority.Adopted != "P2" || !priority.Fixed || priority.Origin != "external" {
		t.Fatal("external human priority not fixed")
	}
	observed, err = remote.ReadPriority(ctx, w, connection.ID, issue.Number, 1)
	if err != nil || observed.Body != issue.Body || observed.State != issue.State {
		t.Fatal("label synchronization changed body/state", err)
	}
	var attempts int
	if err = store.Database().QueryRow(ctx, `SELECT count(*) FROM triage_model_attempts`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatal("label reconciliation called model", attempts, err)
	}
	report["external_fixed"] = true
	report["body_state_unchanged"] = true
	report["priority"] = priority.Adopted
	t.Logf("limited App label export, external human import/fix and unchanged body/state passed: %s", issue.URL)
}
