package workflows

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func TestWorkflowsPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:workflows")
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
	a := access.New(store, []byte(strings.Repeat("w", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.Header.Get("X-Fixture-User")
		if id == "" {
			w.WriteHeader(401)
		}
		return id, id != ""
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	rs := runs.New(store, a, c)
	s := New(store, a, c, rs)
	mux := http.NewServeMux()
	s.Register(mux)
	rs.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprintf("workflow-%d", serial), ExpectedVersion: version}
	}
	call := func(user, method, path string, value any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if value != nil {
			reader = bytes.NewReader(raw(value))
		}
		r, _ := http.NewRequest(method, server.URL+path, reader)
		r.Header.Set("X-Fixture-User", user)
		res, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, b
	}
	base := "/workflows/workspaces/one"
	accept := func(user, path string, value any) Ref {
		t.Helper()
		code, b := call(user, "POST", base+path, value)
		var response apicontract.Response[apicontract.Reference]
		_ = json.Unmarshal(b, &response)
		if code != 200 || response.Data == nil {
			t.Fatalf("%s: %d %s", path, code, b)
		}
		return Ref{ID: response.Data.ID, Version: response.Data.Version}
	}
	definition := plainDefinition()
	request := Write{Mutation: mutation("0"), Value: raw(definition)}
	first := accept("editor", "/records/run-definition:plain", request)
	if replay := accept("editor", "/records/run-definition:plain", request); replay != first {
		t.Fatal("definition receipt changed")
	}
	for _, user := range []string{"viewer", "outsider"} {
		if code, _ := call(user, "POST", base+"/records/run-definition:denied", Write{Mutation: mutation("0"), Value: raw(definition)}); code != 403 {
			t.Fatal("definition write granted", user, code)
		}
	}
	if code, _ := call("outsider", "GET", base+"/records/"+first.ID+"?version="+first.Version, nil); code != 403 {
		t.Fatal("foreign definition read", code)
	}
	verification := accept("editor", "/verify", Verify{Mutation: mutation("0"), Definition: first})
	adoption := accept("editor", "/adopt", Adopt{Mutation: mutation("0"), Definition: first, Validation: verification})
	if adoption.ID != "adoption:"+first.ID {
		t.Fatal(adoption)
	}
	sourceBefore, err := s.Read(ctx, access.Principal{UserID: "admin"}, "one", first)
	if err != nil {
		t.Fatal(err)
	}
	code, exported := call("editor", "GET", base+"/definitions/"+first.ID+"/export?version="+first.Version, nil)
	if code != 200 || !bytes.Equal(exported, sourceBefore.Value) {
		t.Fatal("file/API definition differs", code)
	}
	definition.Name = "new draft"
	second := accept("editor", "/records/"+first.ID, Write{Mutation: mutation(first.Version), Value: raw(definition)})
	adopted, err := s.Read(ctx, access.Principal{UserID: "admin"}, "one", Ref{ID: adoption.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	var pin Adoption
	_ = json.Unmarshal(adopted.Value, &pin)
	if pin.Definition != first {
		t.Fatal("saving changed adopted version")
	}
	old, _ := s.Read(ctx, access.Principal{UserID: "admin"}, "one", first)
	if !bytes.Equal(old.Value, sourceBefore.Value) || first == second {
		t.Fatal("immutable definition changed")
	}
	stale := Write{Mutation: mutation(first.Version), Value: raw(definition)}
	if code, _ := call("editor", "POST", base+"/records/"+first.ID, stale); code != 409 {
		t.Fatal("stale edit overwrote current", code)
	}
	inspection := plainDefinition()
	inspection.Inputs = []string{"prompt"}
	inspection.Spec.Input.Command.Binding = map[string]string{"prompt": "stdin"}
	inspection.Spec.Derivation = &runs.Contract{Name: "inspection", Version: "v1", VerdictField: "decision"}
	inspectionRef := accept("editor", "/records/run-definition:inspection", Write{Mutation: mutation("0"), Value: raw(inspection)})
	graph := graphDefinition()
	graph.Nodes[0].Definition = &first
	graph.Nodes[1].Definition = &inspectionRef
	graphRef := accept("editor", "/records/workflow-definition:graph", Write{Mutation: mutation("0"), Value: raw(graph)})
	v, err := s.Validate(ctx, access.Principal{UserID: "editor"}, "one", graphRef)
	if err != nil || v.Valid {
		t.Fatal("unconnected input accepted", v, err)
	}
	graph.Nodes[1].Inputs = []string{"prompt"}
	graphRef = accept("editor", "/records/workflow-definition:graph", Write{Mutation: mutation(graphRef.Version), Value: raw(graph)})
	v, err = s.Validate(ctx, access.Principal{UserID: "viewer"}, "one", graphRef)
	if err != nil || !v.Valid {
		t.Fatal("readonly validation denied", v, err)
	}
	exec(`INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('worker','one','admin','worker','worker','g',true,true)`)
	mac := hmac.New(sha256.New, []byte(strings.Repeat("w", 32)))
	_, _ = mac.Write([]byte("device\x00worker\x00g"))
	device, err := a.DevicePrincipal(ctx, "one", "d.worker."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	contact, err := rs.ContactRunner(ctx, device, "one", runs.RunnerContact{Mutation: mutation("0"), RunnerID: "fixed-runner", Connections: []runs.Connection{{Kind: "cli", Target: "fixture", Version: "fixture-v1", Environment: "fixed", Subject: "fixed-cli", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"pause", "conditional-write"}, Users: []string{"admin", "editor"}, Repositories: []string{"fixture-repo"}}}})
	if err != nil || contact.Data == nil {
		t.Fatal(contact, err)
	}
	capability := plainDefinition()
	missingSource := plainDefinition()
	missingSource.Spec.Sources = []Ref{{ID: first.ID, Version: "missing-version"}}
	missingSourceRef := accept("editor", "/records/run-definition:missing-source", Write{Mutation: mutation("0"), Value: raw(missingSource)})
	unavailable, sourceErr := s.Validate(ctx, access.Principal{UserID: "editor"}, "one", missingSourceRef)
	if sourceErr != nil || unavailable.Valid || len(unavailable.Problems) != 1 || unavailable.Problems[0].Code != "reference_unavailable" {
		t.Fatal("missing fixed source not detected before execution", unavailable, sourceErr)
	}
	capability.DeviceID = "worker"
	capability.Required = []string{"unavailable"}
	capabilityRef := accept("editor", "/records/run-definition:capability", Write{Mutation: mutation("0"), Value: raw(capability)})
	caps, err := s.Validate(ctx, access.Principal{UserID: "editor"}, "one", capabilityRef)
	if err != nil || caps.Valid || len(caps.Problems) != 1 || caps.Problems[0].Code != "capability_missing" {
		t.Fatal("missing capability not detected", caps, err)
	}
	fixedCaps, err := s.Capabilities(ctx, access.Principal{UserID: "editor"}, "one", "worker", contact.Data.Version)
	if err != nil || fixedCaps.Ref.Version != contact.Data.Version || fixedCaps.Connections[0].Version != "fixture-v1" {
		t.Fatal("fixed capability differs", fixedCaps, err)
	}
	page, err := s.Query(ctx, access.Principal{UserID: "viewer"}, "one", Request{Kind: "heads", Type: "run-definition", Limit: 1})
	if err != nil || page.Data == nil || len(page.Data.Items) != 1 || page.Data.Next == "" {
		t.Fatal("bounded metadata listing", page, err)
	}
	var summary map[string]json.RawMessage
	if json.Unmarshal(page.Data.Items[0].Value, &summary) != nil || summary["spec"] != nil {
		t.Fatal("listing fetched definition body")
	}
	// No connection or executor exists in the save/verify/adopt/validate path.
	var count int
	if err = store.Database().QueryRow(ctx, `SELECT count(*) FROM run_records`).Scan(&count); err != nil || count != 0 {
		t.Fatal("definition work created a Run", count, err)
	}
	// Current resource permissions also apply to historical definitions and their dependents.
	t.Run("durable progression", func(t *testing.T) { executionScenario(t, s, device, mutation) })
	t.Run("external conditions", func(t *testing.T) { externalScenario(t, s, device, mutation) })
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id='one' AND id=$1`, first.ID)
	if _, err = s.Read(ctx, access.Principal{UserID: "viewer"}, "one", graphRef); err == nil {
		t.Fatal("reference granted private definition")
	}
	t.Log("versioned definition/file roundtrip, immutable adoption, CAS/replay, pure validation, bounded summaries and current dependency policy passed; Run count 0")
}
