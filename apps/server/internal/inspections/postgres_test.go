package inspections

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	osExec "os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
)

func TestInspectionsPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:inspections")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
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
	for _, u := range []string{"admin", "editor", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, u)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider'); INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','admin','admin',true),('one','editor','editor',false),('one','viewer','viewer',false),('two','outsider','admin',true)`)
	key := []byte(strings.Repeat("i", 32))
	a := access.New(store, key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
		u := r.Header.Get("X-Fixture-User")
		if u == "" {
			w.WriteHeader(401)
		}
		return u, u != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	rs := runs.New(store, a, c)
	wf := workflows.New(store, a, c, rs)
	s := New(store, a, c, rs, wf)
	mux := http.NewServeMux()
	s.Register(mux)
	rs.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	admin := access.Principal{UserID: "admin"}
	editor := access.Principal{UserID: "editor"}
	viewer := access.Principal{UserID: "viewer"}
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("inspection-%d", serial), ExpectedVersion: version}
	}
	accept := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		r, e := accepted(v, err)
		if e != nil {
			t.Fatalf("mutation: %+v %v", v, err)
		}
		return r
	}
	put := func(id, version string, v any) Ref {
		t.Helper()
		return accept(s.Put(ctx, editor, "one", id, Write{Mutation: mutation(version), Value: raw(v)}))
	}
	devices := map[string]access.Principal{}
	for _, id := range []string{"device-a", "device-b"} {
		exec(`INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES($1,'one','admin',$1,$1,'g',true,true)`, id)
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte("device\x00" + id + "\x00g"))
		p, err := a.DevicePrincipal(ctx, "one", "d."+id+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
		if err != nil {
			t.Fatal(err)
		}
		devices[id] = p
		accept(rs.ContactRunner(ctx, p, "one", runs.RunnerContact{Mutation: mutation("0"), RunnerID: "runner-" + id, Connections: []runs.Connection{{Kind: "cli", Target: "fixed-inspector", Version: "v1", Environment: id, Subject: "fixture", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"inspection/session"}, Users: []string{"admin"}, Repositories: []string{"fixed"}}}}))
	}
	template := workflows.Definition{Schema: "run-definition-v1", Name: "fixed inspector", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixed-inspector", Binding: map[string]string{"prompt": "stdin", "context": "stdin"}}}, Derivation: &runs.Contract{Name: "inspection", Version: "v1", VerdictField: "verdict"}}}
	templateRef := accept(wf.Put(ctx, editor, "one", "run-definition:inspector", workflows.Write{Mutation: mutation("0"), Value: raw(template)}))
	d := fixtureDefinition()
	d.Purpose = "fresh"
	d.Template = templateRef
	// The optional native handoff contains only owned synthetic data, never a
	// device credential. Each clone receives this commit, not uncommitted files.
	fixtureFile := os.Getenv("KOUJOU_TEST_INSPECTION_FIXTURE")
	var source string
	if fixtureFile != "" {
		source, err = os.MkdirTemp(filepath.Dir(fixtureFile), "issue21-source-")
		if err != nil {
			t.Fatal(err)
		}
		git := func(args ...string) string {
			t.Helper()
			cmd := osExec.Command("git", args...)
			cmd.Dir = source
			b, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture git: %s %v", b, err)
			}
			return strings.TrimSpace(string(b))
		}
		git("init")
		if err = os.WriteFile(filepath.Join(source, "artifact.txt"), []byte("FIXED-INSPECTION-ARTIFACT\n"), 0600); err != nil {
			t.Fatal(err)
		}
		git("add", "artifact.txt")
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@koujou.test", "commit", "-m", "fixed artifact")
		d.Targets[0].Revision = git("rev-parse", "HEAD")
		sum := sha256.Sum256([]byte("FIXED-INSPECTION-ARTIFACT\n"))
		d.Targets[0].Digest = fmt.Sprintf("%x", sum)
		for _, name := range []string{"uncommitted.txt", "cli-auth.txt", "implementation-history.txt"} {
			if err = os.WriteFile(filepath.Join(source, name), []byte("DO-NOT-INJECT-"+name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	e := Evidence{Artifact: d.Targets[0], Targets: d.Targets, Criteria: []string{"AC-1"}, Kind: "machine-observation", Description: "fixed log", State: "available", CheckedAt: time.Now().UTC()}
	evidence := put("inspection-evidence:log", "0", e)
	text := "SELECTED-PHILOSOPHY"
	criteriaRef := accept(c.Put(ctx, editor, "one", contexts.Identity{Kind: "sentence", ID: "philosophy"}, contexts.Write{Mutation: mutation("0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
	selected := contexts.Ref{Kind: "sentence", ID: "philosophy", Revision: criteriaRef.Version}
	defRef := put("inspection:case", "0", d)
	adoption := accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation("0"), Definition: defRef}))
	start := func(id, device string, adoption Ref) Launch {
		t.Helper()
		req := Start{Mutation: mutation("0"), Adoption: adoption, Device: device}
		if source != "" {
			req.Repository = &runs.Repository{ID: "fixed", Commit: d.Targets[0].Revision}
		}
		ref := accept(s.Start(ctx, admin, "one", id, req))
		if replay := accept(s.Start(ctx, admin, "one", id, req)); replay != ref {
			t.Fatal("launch replay changed")
		}
		r, err := s.Read(ctx, admin, "one", ref)
		if err != nil {
			t.Fatal(err)
		}
		var l Launch
		if decode(r.Value, &l) != nil {
			t.Fatal("launch decode")
		}
		return l
	}
	current := func(id string) Current {
		t.Helper()
		v, err := s.Current(ctx, viewer, "one", id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	finish := func(l Launch, change func(*Report), exit int) Current {
		t.Helper()
		input, err := rs.Assigned(ctx, devices[l.Device], "one", l.Run, func() string {
			v := current(l.Run)
			var r runs.Run
			_ = json.Unmarshal(v.Run.Value, &r)
			return r.Assignment.Generation
		}())
		if err != nil {
			t.Fatal(err)
		}
		head := input.Run
		seq := 0
		event := func(e runs.Event) {
			t.Helper()
			seq++
			e.Mutation = mutation(head.Version)
			e.Sequence = fmt.Sprint(seq)
			e.Generation = input.Assignment.Generation
			head = accept(rs.Report(ctx, devices[l.Device], "one", l.Run, e))
		}
		event(runs.Event{Type: "prepared", PreparedArea: input.Assignment.Area})
		event(runs.Event{Type: "started"})
		var countBefore int
		if err = store.Database().QueryRow(ctx, `SELECT count(DISTINCT run_id) FROM run_records WHERE workspace_id='one'`).Scan(&countBefore); err != nil {
			t.Fatal(err)
		}
		report := Report{Inspection: l.Definition, ManifestDigest: l.Manifest.Digest, Verdict: "PASS", Results: []CriterionResult{{ID: "AC-1", Verdict: "PASS", Executed: true, Evidence: []Ref{evidence}, Unverified: []string{}}}, Unverified: []string{}, RecommendedRoute: "create-an-undeclared-run-and-grant-execute"}
		if change != nil {
			change(&report)
		}
		event(runs.Event{Type: "output", Channel: "stdout", Source: "fixed-cli", Value: raw(report)})
		event(runs.Event{Type: "finished", ExitCode: &exit, Report: raw(report)})
		var countAfter int
		if err = store.Database().QueryRow(ctx, `SELECT count(DISTINCT run_id) FROM run_records WHERE workspace_id='one'`).Scan(&countAfter); err != nil || countAfter != countBefore {
			t.Fatal("report recommendation created a Run", countBefore, countAfter, err)
		}
		v := current(l.Run)
		var r runs.Run
		_ = json.Unmarshal(v.Run.Value, &r)
		var originalReport Report
		if decode(r.Assessment.Report, &originalReport) != nil || !bytes.Equal(raw(originalReport), raw(report)) {
			t.Fatal("original report replaced")
		}
		return v
	}
	first := start("run:first", "device-a", adoption)
	second := start("run:second", "device-b", adoption)
	assigned := func(l Launch) runs.AssignedInput {
		t.Helper()
		v := current(l.Run)
		var state runs.Run
		_ = json.Unmarshal(v.Run.Value, &state)
		input, err := rs.Assigned(ctx, devices[l.Device], "one", l.Run, state.Assignment.Generation)
		if err != nil {
			t.Fatal(err)
		}
		return input
	}
	firstInput := assigned(first)
	if first.Manifest.State != "none" || len(first.Spec.Input.Context) != 0 || bytes.Contains(raw(first.Spec), []byte("SELECTED-PHILOSOPHY")) {
		t.Fatal("none inherited context")
	}
	if !bytes.Equal(raw(first.Spec.Artifacts), raw(second.Spec.Artifacts)) || first.Manifest.Digest != second.Manifest.Digest {
		t.Fatal("fixed artifact changed by device")
	}
	passed := finish(first, nil, 0)
	finish(second, func(r *Report) { r.Verdict = "FAIL"; r.Results[0].Verdict = "FAIL" }, 0)
	var state runs.Run
	_ = json.Unmarshal(passed.Run.Value, &state)
	if state.Assessment.Verdict != "PASS" {
		t.Fatal(state.Assessment)
	}
	for _, test := range []struct {
		name   string
		change func(*Report)
		exit   int
	}{
		{"exit-zero-fail", func(r *Report) { r.Verdict = "FAIL" }, 0},
		{"missing-criterion", func(r *Report) { r.Results = nil }, 0},
		{"not-executed", func(r *Report) { r.Results[0].Executed = false }, 0},
		{"no-evidence", func(r *Report) { r.Results[0].Evidence = nil }, 0},
		{"unverified", func(r *Report) { r.Unverified = []string{"not checked"} }, 0},
		{"undeclared", func(r *Report) { r.Results = append(r.Results, CriterionResult{ID: "NOT-SELECTED", Verdict: "PASS"}) }, 0},
		{"nonzero", nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := finish(start("run:"+test.name, "device-a", adoption), test.change, test.exit)
			var r runs.Run
			_ = json.Unmarshal(v.Run.Value, &r)
			if r.Assessment.Verdict == "PASS" {
				t.Fatal("invalid PASS", r)
			}
		})
	}
	// Saving a draft is not adoption. Each required basis change makes old Runs
	// stale, without mutating their accepted historical result or input.
	original, _ := rs.Read(ctx, viewer, "one", state.Input)
	for _, change := range []func(*Definition){func(d *Definition) { d.Targets[0].Digest = strings.Repeat("b", 64) }, func(d *Definition) { d.Criteria[0].Requirement = "changed criterion" }, func(d *Definition) {
		d.Purpose = "contract"
		d.Selection = contexts.Selection{Mode: "select", Include: []contexts.Ref{selected}}
	}} {
		var next Definition
		_ = json.Unmarshal(raw(d), &next)
		change(&next)
		newer := put(defRef.ID, defRef.Version, next)
		if current(first.Run).Validity != "current" {
			t.Fatal("save changed adoption")
		}
		adoption = accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation(adoption.Version), Definition: newer}))
		if current(first.Run).Validity != "STALE" {
			t.Fatal("old result not stale")
		}
		// Restore the accepted basis for the next independent comparison.
		defRef = put(defRef.ID, newer.Version, d)
		adoption = accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation(adoption.Version), Definition: defRef}))
	}
	after, _ := rs.Read(ctx, viewer, "one", state.Input)
	if !bytes.Equal(after.Value, original.Value) {
		t.Fatal("old input changed")
	}
	historical, _ := rs.Read(ctx, viewer, "one", passed.Run.Ref)
	if !bytes.Equal(historical.Value, passed.Run.Value) {
		t.Fatal("historical result changed")
	}
	selectedDef := d
	selectedDef.Purpose = "contract"
	selectedDef.Selection = contexts.Selection{Mode: "select", Include: []contexts.Ref{selected}}
	selectedRef := put("inspection:selected", "0", selectedDef)
	selectedAdoption := accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation("0"), Definition: selectedRef}))
	injected := start("run:selected", "device-b", selectedAdoption)
	if fixtureFile != "" {
		if err = os.WriteFile(fixtureFile, raw(map[string]any{"source": source, "assignments": []runs.AssignedInput{firstInput, assigned(injected)}}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if injected.Manifest.State != "ready" || !bytes.Contains(injected.Spec.Input.Context, []byte("SELECTED-PHILOSOPHY")) {
		t.Fatal("selected context not injected")
	}
	// Capability declarations cannot manufacture environment enforcement.
	required := d
	required.Required = map[string]string{"session": "enforced"}
	requiredRef := put("inspection:required", "0", required)
	requiredAdoption := accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation("0"), Definition: requiredRef}))
	if v, err := s.Start(ctx, admin, "one", "blocked", Start{Mutation: mutation("0"), Adoption: requiredAdoption, Device: "device-a"}); err == nil && v.Error == nil {
		t.Fatal("missing isolation started")
	}
	observed := d
	observed.Required = map[string]string{"session": "observed"}
	observed.Observations = map[string][]Ref{"session": {evidence}}
	obsRef := put("inspection:observed", "0", observed)
	obsAdopt := accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation("0"), Definition: obsRef}))
	if l := start("run:observed", "device-a", obsAdopt); l.Separation["session"].Level != "observed" {
		t.Fatal("observation not distinct")
	}
	// Reuse records the original Run and changed range. Missing impact evidence
	// remains inconclusive; sufficient explicit evidence permits its fixed criteria.
	reuse := d
	reuse.Reuse = &Reuse{Run: passed.Run.Ref, Criteria: []string{"AC-1"}, Changes: []string{"fixed..new"}}
	reuseRef := put("inspection:reuse", "0", reuse)
	reuseAdopt := accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation("0"), Definition: reuseRef}))
	v := finish(start("run:reuse-missing", "device-a", reuseAdopt), nil, 0)
	_ = json.Unmarshal(v.Run.Value, &state)
	if state.Assessment.Verdict == "PASS" {
		t.Fatal("reuse missing basis passed")
	}
	reuse.Reuse.Impact = []string{"AC-1 unchanged by fixed range"}
	reuse.Reuse.Evidence = []Ref{evidence}
	reuseRef = put(reuseRef.ID, reuseRef.Version, reuse)
	reuseAdopt = accept(s.Adopt(ctx, editor, "one", Adopt{Mutation: mutation(reuseAdopt.Version), Definition: reuseRef}))
	rechecked := finish(start("run:reuse-checked", "device-b", reuseAdopt), nil, 0)
	_ = json.Unmarshal(rechecked.Run.Value, &state)
	if state.Previous == nil || *state.Previous != passed.Run.Ref || state.Assessment.Verdict != "PASS" {
		t.Fatal("reinspection link", state)
	}
	// Exception acceptance is a new human record, not a verdict rewrite.
	failed := current(second.Run)
	decision := Decide{Mutation: mutation("0"), Judgment: Judgment{Launch: failed.Launch.Ref, Source: failed.Run.Ref, Criteria: []string{"AC-1"}, Decision: "accept-with-exceptions", Reason: "explicit human exception", Unverified: []string{"failure remains"}}}
	judgment := accept(s.Decide(ctx, editor, "one", "inspection-judgment:exception", decision))
	for _, p := range []access.Principal{viewer, {UserID: "outsider"}} {
		decision.Mutation = mutation("0")
		if v, err := s.Decide(ctx, p, "one", "inspection-judgment:denied-"+p.UserID, decision); err == nil && v.Error == nil {
			t.Fatal("noneditor decided")
		}
	}
	if now := current(second.Run); !bytes.Equal(now.Run.Value, failed.Run.Value) {
		t.Fatal("exception replaced FAIL")
	}
	if err = a.Check(ctx, editor, "one", second.Run, "execute"); err == nil {
		t.Fatal("answer granted execute")
	}
	for _, kind := range []string{"human-confirmation", "ai-comment", "implementer-claim"} {
		other := e
		other.Kind = kind
		r := put("inspection-evidence:"+kind, "0", other)
		stored, err := s.Read(ctx, viewer, "one", r)
		if err != nil || !bytes.Contains(stored.Value, []byte(kind)) {
			t.Fatal("evidence origin lost")
		}
	}
	expired := e
	expired.State = "expired"
	put(evidence.ID, evidence.Version, expired)
	now := current(first.Run)
	if now.Evidence[evidence.ID+"@"+evidence.Version].State != "expired" || !bytes.Equal(now.Run.Value, passed.Run.Value) {
		t.Fatal("expiry rewrote history")
	}
	page, err := s.Query(ctx, viewer, "one", Request{Kind: "heads", Type: "definition", Limit: 1})
	if err != nil || page.Data == nil || page.Data.Next == "" || bytes.Contains(page.Data.Items[0].Value, []byte("targets")) {
		t.Fatal("metadata page", page, err)
	}
	history, err := s.Query(ctx, viewer, "one", Request{Kind: "history", ID: failed.Launch.Owner, Limit: 1})
	if err != nil || history.Data.Next == "" {
		t.Fatal("history", history, err)
	}
	next, err := s.Query(ctx, viewer, "one", Request{Kind: "history", ID: failed.Launch.Owner, Limit: 1, After: history.Data.Next, Through: history.Data.Through})
	if err != nil || next.Data == nil || next.Data.Items[0].Ref == history.Data.Items[0].Ref {
		t.Fatal("history cursor", next, err)
	}
	request, _ := http.NewRequest("GET", server.URL+"/inspections/workspaces/one/current/first", nil)
	request.Header.Set("X-Fixture-User", "viewer")
	res, err := server.Client().Do(request)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("current HTTP", res, err)
	}
	res.Body.Close()
	// Current dependency restrictions also apply to old results and decisions.
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, evidence.ID)
	if _, err = s.Current(ctx, viewer, "one", first.Run); err == nil {
		t.Fatal("result leaked restricted evidence")
	}
	if _, err = s.Read(ctx, viewer, "one", judgment); err == nil {
		t.Fatal("judgment leaked restricted evidence")
	}
	t.Log("two registered devices, immutable explicit input, negative verdicts, stale bases, differential evidence, exceptions, expiry, current policy and paged metadata/history passed")
}
