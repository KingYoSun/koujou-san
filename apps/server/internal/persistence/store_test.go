package persistence

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All routes and identities below are fixtures in the test binary only. The
// product handler never registers these unauthenticated domain test endpoints.
func TestHTTPHelper(t *testing.T) {
	if os.Getenv("KOUJOU_HTTP_FIXTURE") != "1" {
		t.Skip("fixture subprocess only")
	}
	store, err := Open(context.Background(), os.Getenv("KOUJOU_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.pool.Exec(context.Background(), `CREATE TABLE IF NOT EXISTS fixture_records (workspace_id text NOT NULL, id text NOT NULL, version text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(workspace_id,id))`)
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]Scope{
		"actor-a":   {ActorID: "actor-a", Grant: apicontract.Grant{WorkspaceID: "workspace-a", CanRead: true, CanUpdate: true}},
		"actor-a2":  {ActorID: "actor-a2", Grant: apicontract.Grant{WorkspaceID: "workspace-a", CanRead: true, CanUpdate: true}},
		"reader-a":  {ActorID: "reader-a", Grant: apicontract.Grant{WorkspaceID: "workspace-a", CanRead: true}},
		"revoked-a": {ActorID: "actor-a", Grant: apicontract.Grant{WorkspaceID: "workspace-a", CanRead: true}},
		"actor-b":   {ActorID: "actor-b", Grant: apicontract.Grant{WorkspaceID: "workspace-b", CanRead: true, CanUpdate: true}},
		"actor-f":   {ActorID: "actor-f", Grant: apicontract.Grant{WorkspaceID: "workspace-feed", CanRead: true, CanUpdate: true}},
	}
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(store.Ready))
	mux.HandleFunc("POST /mutate", func(w http.ResponseWriter, r *http.Request) {
		var command Command
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			http.Error(w, "invalid fixture", 400)
			return
		}
		response, _ := store.Apply(r.Context(), scopes[r.Header.Get("Fixture-Actor")], command, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
			if command.Deleted {
				_, err := tx.Exec(r.Context(), "DELETE FROM fixture_records WHERE workspace_id=$1 AND id=$2", ref.WorkspaceID, ref.ID)
				return json.RawMessage("null"), err
			}
			_, err := tx.Exec(r.Context(), `INSERT INTO fixture_records VALUES ($1,$2,$3,$4) ON CONFLICT(workspace_id,id) DO UPDATE SET version=EXCLUDED.version,payload=EXCLUDED.payload`, ref.WorkspaceID, ref.ID, ref.Version, command.Payload)
			return command.Payload, err
		})
		if response.Data != nil && r.URL.Query().Get("drop_response") == "1" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		status := 200
		if response.Error != nil {
			status = 409
			switch response.Error.Code {
			case "forbidden":
				status = 403
			case "unavailable":
				status = 503
			case "invalid_request":
				status = 400
			}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		response, _ := store.Version(r.Context(), scopes[r.Header.Get("Fixture-Actor")], r.URL.Query().Get("workspace"), r.URL.Query().Get("id"))
		status := 200
		if response.Error != nil {
			status = 400
			if response.Error.Code == "forbidden" {
				status = 403
			}
			if response.Error.Code == "unavailable" {
				status = 503
			}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("GET /changes", func(w http.ResponseWriter, r *http.Request) {
		response, _ := store.Changes(r.Context(), scopes[r.Header.Get("Fixture-Actor")], r.URL.Query().Get("workspace"), r.URL.Query().Get("after"), 1)
		_ = json.NewEncoder(w).Encode(response)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": "http://" + listener.Addr().String()})
	if err = http.Serve(listener, mux); err != nil {
		t.Fatal(err)
	}
}

type fixtureProcess struct {
	url     string
	process *exec.Cmd
}

func startFixture(t *testing.T, databaseURL string) *fixtureProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestHTTPHelper$")
	command.Env = append(os.Environ(), "KOUJOU_HTTP_FIXTURE=1", "KOUJOU_TEST_DATABASE_URL="+databaseURL)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	fixture := &fixtureProcess{process: command}
	t.Cleanup(func() { fixture.stop() })
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			var body map[string]string
			if json.Unmarshal(scanner.Bytes(), &body) == nil && body["url"] != "" {
				ready <- body["url"]
				return
			}
		}
		ready <- ""
	}()
	select {
	case fixture.url = <-ready:
		if fixture.url == "" {
			t.Fatal("fixture API failed to start")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fixture API startup timeout")
	}
	return fixture
}

func (fixture *fixtureProcess) stop() {
	if fixture.process != nil {
		_ = fixture.process.Process.Kill()
		_ = fixture.process.Wait()
		fixture.process = nil
	}
}

func exchange[T any](client *http.Client, base, method, path, actor string, body any) (apicontract.Response[T], int, error) {
	var result apicontract.Response[T]
	raw, err := json.Marshal(body)
	if err != nil {
		return result, 0, err
	}
	request, err := http.NewRequest(method, base+path, bytes.NewReader(raw))
	if err != nil {
		return result, 0, err
	}
	request.Header.Set("Fixture-Actor", actor)
	response, err := client.Do(request)
	if err != nil {
		return result, 0, err
	}
	defer response.Body.Close()
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, response.StatusCode, err
}

func mutate(t *testing.T, fixture *fixtureProcess, actor string, command Command) apicontract.Response[apicontract.Reference] {
	t.Helper()
	response, _, err := exchange[apicontract.Reference](&http.Client{Timeout: 5 * time.Second}, fixture.url, "POST", "/mutate", actor, command)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func command(workspace, id, operation, expected, value string) Command {
	return Command{WorkspaceID: workspace, ResourceID: id, Kind: "fixture", Mutation: apicontract.Mutation{OperationID: operation, ExpectedVersion: expected}, Payload: json.RawMessage(`{"value":"` + value + `"}`)}
}

func docker(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	process := exec.Command("docker", args...)
	if input != nil {
		process.Stdin = bytes.NewReader(input)
	}
	var stderr bytes.Buffer
	process.Stderr = &stderr
	result, err := process.Output()
	if err != nil {
		t.Fatalf("fixture docker operation failed: %v: %s", err, stderr.String())
	}
	return result
}

func testPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal("invalid fixture DB configuration")
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPostgres(t *testing.T) {
	databaseURL := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("run pnpm run test:database")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("fixture PostgreSQL must be loopback")
	}
	container, project := os.Getenv("KOUJOU_TEST_CONTAINER"), os.Getenv("KOUJOU_TEST_PROJECT")
	if container == "" || !strings.HasPrefix(project, "koujou-san-db-test-") {
		t.Fatal("managed fixture container is required")
	}
	label := strings.TrimSpace(string(docker(t, nil, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project"}}`, container)))
	if label != project {
		t.Fatal("refusing control of another PostgreSQL project")
	}
	ctx := context.Background()
	pool := testPool(t, databaseURL)
	sql := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var value int
		if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	newDatabase := func(name string) string {
		t.Helper()
		docker(t, nil, "exec", container, "createdb", "-U", "postgres", "-O", "koujou", name)
		target := *parsed
		target.Path = "/" + name
		return target.String()
	}
	schema := func(target *pgxpool.Pool) string {
		t.Helper()
		var result string
		err := target.QueryRow(ctx, `SELECT string_agg(table_name||':'||column_name||':'||data_type||':'||is_nullable,',' ORDER BY table_name,ordinal_position) FROM information_schema.columns WHERE table_schema='public' AND table_name LIKE 'shared_%'`).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	sql("CREATE TABLE baseline_fixture (id text PRIMARY KEY)")
	sql("INSERT INTO baseline_fixture VALUES ('bootstrap-data')")
	if err = migrate(ctx, pool, []string{initialSchema}); err != nil {
		t.Fatal(err)
	}
	originalSchema := schema(pool)
	if err = migrate(ctx, pool, []string{initialSchema}); err != nil {
		t.Fatal(err)
	}
	if count("SELECT count(*) FROM shared_migrations") != 1 || count("SELECT count(*) FROM baseline_fixture") != 1 || schema(pool) != originalSchema {
		t.Fatal("migration rerun changed schema or existing data")
	}
	fresh := testPool(t, newDatabase("koujou_empty"))
	if err = migrate(ctx, fresh, []string{initialSchema}); err != nil {
		t.Fatal(err)
	}
	if schema(fresh) != originalSchema {
		t.Fatal("fresh and bootstrap migration schema differ")
	}
	broken := testPool(t, newDatabase("koujou_migration_failure"))
	if err = migrate(ctx, broken, []string{initialSchema, "CREATE TABLE doomed (id integer); SELECT 1/0"}); err == nil {
		t.Fatal("injected migration error was ignored")
	}
	var absent bool
	if err = broken.QueryRow(ctx, "SELECT to_regclass('shared_streams') IS NULL AND to_regclass('shared_migrations') IS NULL AND to_regclass('doomed') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("failed migration left partial schema")
	}
	if err = migrate(ctx, pool, []string{initialSchema + "\n-- changed applied source"}); err == nil {
		t.Fatal("applied migration edit was ignored")
	}
	t.Log("AC-1: empty/bootstrap/current DB migration and failure rollback passed")

	fixture := startFixture(t, databaseURL)
	firstCommand := command("workspace-a", "resource-a", "op-create", "0", "initial")
	first := mutate(t, fixture, "actor-a", firstCommand)
	if first.Data == nil || first.Error != nil {
		t.Fatalf("create failed: %+v", first.Error)
	}
	before := count("SELECT count(*) FROM shared_changes")
	type concurrentResult struct {
		response apicontract.Response[apicontract.Reference]
		status   int
		err      error
	}
	parallel := func(commands []Command) []concurrentResult {
		t.Helper()
		results := make(chan concurrentResult, len(commands))
		for _, request := range commands {
			go func() {
				response, status, err := exchange[apicontract.Reference](&http.Client{Timeout: 5 * time.Second}, fixture.url, "POST", "/mutate", "actor-a", request)
				results <- concurrentResult{response, status, err}
			}()
		}
		collected := []concurrentResult{}
		for range commands {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			collected = append(collected, result)
		}
		return collected
	}
	for _, result := range parallel([]Command{firstCommand, firstCommand}) {
		if result.status != 200 || !reflect.DeepEqual(result.response, first) {
			t.Fatal("replay did not return identical committed result")
		}
	}
	if count("SELECT count(*) FROM shared_changes") != before {
		t.Fatal("replay appended change records")
	}
	changed := firstCommand
	changed.Payload = json.RawMessage(`{"value":"different"}`)
	conflict := mutate(t, fixture, "actor-a", changed)
	if conflict.Error == nil || conflict.Error.Code != "operation_conflict" {
		t.Fatal("operation ID accepted changed content")
	}
	winners := 0
	losers := 0
	var winner apicontract.Response[apicontract.Reference]
	for _, result := range parallel([]Command{command("workspace-a", "resource-a", "op-a", first.Data.Version, "choice-a"), command("workspace-a", "resource-a", "op-b", first.Data.Version, "choice-b")}) {
		if result.status == 200 && result.response.Data != nil {
			winners++
			winner = result.response
		} else if result.status == 409 && result.response.Error != nil && result.response.Error.Code == "version_conflict" && result.response.Error.CurrentVersion != "" {
			losers++
		} else {
			t.Fatalf("unexpected race result: %+v", result.response)
		}
	}
	if winners != 1 || losers != 1 || count("SELECT count(*) FROM shared_changes") != before+1 {
		t.Fatal("concurrent CAS accepted more than one update")
	}
	lost := command("workspace-a", "resource-a", "op-lost", winner.Data.Version, "response-lost")
	_, _, err = exchange[apicontract.Reference](&http.Client{Timeout: 5 * time.Second}, fixture.url, "POST", "/mutate?drop_response=1", "actor-a", lost)
	if err == nil {
		t.Fatal("response loss was not injected")
	}
	var saved json.RawMessage
	if err = pool.QueryRow(ctx, "SELECT result FROM shared_receipts WHERE workspace_id='workspace-a' AND actor_id='actor-a' AND operation_id='op-lost'").Scan(&saved); err != nil {
		t.Fatal("lost response had no committed receipt")
	}
	var lostResult apicontract.Response[apicontract.Reference]
	if err = json.Unmarshal(saved, &lostResult); err != nil {
		t.Fatal(err)
	}
	if replay := mutate(t, fixture, "actor-a", lost); !reflect.DeepEqual(replay, lostResult) {
		t.Fatal("lost-response replay differs")
	}
	otherActor := mutate(t, fixture, "actor-a2", command("workspace-a", "resource-a2", "op-create", "0", "other-actor"))
	if otherActor.Data == nil {
		t.Fatal("operation IDs were not scoped to actor")
	}
	t.Log("AC-3: parallel replay/CAS, operation content and actor scope, response loss passed")

	denied := mutate(t, fixture, "reader-a", command("workspace-a", "resource-a", "reader-write", lostResult.Data.Version, "denied"))
	if denied.Error == nil || denied.Error.Code != "forbidden" {
		t.Fatal("read permission granted update")
	}
	revoked := mutate(t, fixture, "revoked-a", firstCommand)
	if revoked.Data != nil || revoked.Error == nil || revoked.Error.Code != "forbidden" {
		t.Fatal("revoked update grant replayed an earlier authorized result")
	}
	ownB := mutate(t, fixture, "actor-b", command("workspace-b", "resource-b", "op-b-create", "0", "private-b"))
	if ownB.Data == nil {
		t.Fatal("workspace B create failed")
	}
	denied = mutate(t, fixture, "actor-a", command("workspace-b", "resource-b", "cross-workspace", ownB.Data.Version, "denied"))
	if denied.Error == nil || denied.Error.Code != "forbidden" {
		t.Fatal("cross-workspace update was allowed")
	}
	for _, target := range []struct {
		workspace, id string
		code          string
	}{{"workspace-b", "resource-b", "forbidden"}, {"workspace-a", "resource-b", "invalid_request"}} {
		response, _, err := exchange[apicontract.Reference](&http.Client{Timeout: 5 * time.Second}, fixture.url, "GET", "/version?workspace="+target.workspace+"&id="+target.id, "actor-a", nil)
		if err != nil || response.Data != nil || response.Error == nil || response.Error.Code != target.code {
			t.Fatal("foreign resource ID exposed Workspace data")
		}
	}
	if count("SELECT count(*) FROM shared_receipts WHERE operation_id IN ('cross-workspace','reader-write')") != 0 {
		t.Fatal("denied writes persisted receipts")
	}
	t.Log("AC-5: cross-Workspace save/read and denied update passed")

	sql(`CREATE FUNCTION fixture_fail_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource_id='resource-a' THEN RAISE EXCEPTION 'fixture injected journal failure'; END IF; RETURN NEW; END $$`)
	sql("CREATE TRIGGER fixture_fail_change BEFORE INSERT ON shared_changes FOR EACH ROW EXECUTE FUNCTION fixture_fail_change()")
	before = count("SELECT count(*) FROM shared_changes")
	rollback := mutate(t, fixture, "actor-a", command("workspace-a", "resource-a", "op-rollback", lostResult.Data.Version, "not-saved"))
	if rollback.Data != nil || rollback.Error == nil || rollback.Error.Code != "unavailable" {
		t.Fatal("failed transaction returned success")
	}
	var version string
	var raw json.RawMessage
	if err = pool.QueryRow(ctx, "SELECT version,payload FROM fixture_records WHERE workspace_id='workspace-a' AND id='resource-a'").Scan(&version, &raw); err != nil {
		t.Fatal(err)
	}
	if version != lostResult.Data.Version || !bytes.Contains(raw, []byte("response-lost")) || count("SELECT count(*) FROM shared_changes") != before || count("SELECT count(*) FROM shared_receipts WHERE operation_id='op-rollback'") != 0 {
		t.Fatal("domain/journal/receipt did not roll back together")
	}
	if err = pool.QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='workspace-a' AND resource_id='resource-a'").Scan(&version); err != nil || version != lostResult.Data.Version {
		t.Fatal("version advanced on rollback")
	}
	sql("DROP TRIGGER fixture_fail_change ON shared_changes")
	sql("DROP FUNCTION fixture_fail_change()")
	t.Log("AC-2: injected journal failure rolled back domain data, version, journal and receipt")

	feed1 := mutate(t, fixture, "actor-f", command("workspace-feed", "resource-feed", "feed-1", "0", "one"))
	feed2 := mutate(t, fixture, "actor-f", command("workspace-feed", "resource-feed", "feed-2", feed1.Data.Version, "two"))
	deleteCommand := command("workspace-feed", "resource-feed", "feed-3", feed2.Data.Version, "unused")
	deleteCommand.Deleted = true
	deleteCommand.Payload = json.RawMessage("null")
	feed3 := mutate(t, fixture, "actor-f", deleteCommand)
	if feed3.Data == nil {
		t.Fatal("delete failed")
	}
	if count("SELECT count(*) FROM fixture_records WHERE workspace_id='workspace-feed' AND id='resource-feed'") != 0 {
		t.Fatal("domain deletion was not applied")
	}
	page := func(base, after string) apicontract.Response[[]Change] {
		t.Helper()
		result, _, err := exchange[[]Change](&http.Client{Timeout: 5 * time.Second}, base, "GET", "/changes?workspace=workspace-feed&after="+url.QueryEscape(after), "actor-f", nil)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	firstPage := page(fixture.url, "")
	if firstPage.Data == nil || len(*firstPage.Data) != 1 || firstPage.Meta.NextCursor == nil || firstPage.Meta.Completeness.State != "partial" || *firstPage.Meta.Position != *feed3.Meta.Position {
		t.Fatal("first page did not freeze at committed position")
	}
	feed4 := mutate(t, fixture, "actor-f", command("workspace-feed", "resource-feed-new", "feed-4", "0", "later"))
	if feed4.Data == nil {
		t.Fatal("page-between write failed")
	}
	originalPID := fixture.process.Process.Pid
	fixture.stop()
	fixture = startFixture(t, databaseURL)
	if fixture.process.Process.Pid == originalPID {
		t.Fatal("API fixture was not restarted")
	}
	if replay := mutate(t, fixture, "actor-a", lost); !reflect.DeepEqual(replay, lostResult) {
		t.Fatal("API restart changed replay result")
	}
	secondPage := page(fixture.url, *firstPage.Meta.NextCursor)
	if secondPage.Data == nil || (*secondPage.Data)[0].Version != feed2.Data.Version || secondPage.Meta.NextCursor == nil {
		t.Fatal("resume page lost update version")
	}
	lastPage := page(fixture.url, *secondPage.Meta.NextCursor)
	if lastPage.Data == nil || len(*lastPage.Data) != 1 || !(*lastPage.Data)[0].Deleted || (*lastPage.Data)[0].Version != feed3.Data.Version || lastPage.Meta.NextCursor != nil || lastPage.Meta.Completeness.State != "complete" || *lastPage.Meta.Position != *feed3.Meta.Position {
		t.Fatal("delete or frozen boundary was lost")
	}
	tail := page(fixture.url, *lastPage.Meta.Position)
	if tail.Data == nil || len(*tail.Data) != 1 || (*tail.Data)[0].Version != feed4.Data.Version {
		t.Fatal("later update was omitted from next range")
	}
	t.Log("AC-4: ordered create/update/delete, frozen pages and API subprocess restart passed")

	backup := docker(t, nil, "exec", container, "pg_dump", "-U", "koujou", "-Fc", "-d", "koujou")
	postBackup := mutate(t, fixture, "actor-f", command("workspace-feed", "post-backup", "feed-5", "0", "not-in-backup"))
	if postBackup.Data == nil {
		t.Fatal("post-backup fixture failed")
	}
	docker(t, nil, "stop", "--time", "2", container)
	outage := mutate(t, fixture, "actor-a", command("workspace-a", "resource-a", "op-outage", lostResult.Data.Version, "not-saved"))
	if outage.Data != nil || outage.Error == nil || outage.Error.Code != "unavailable" {
		t.Fatal("DB outage returned success")
	}
	readiness, err := http.Get(fixture.url + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = readiness.Body.Close()
	if readiness.StatusCode != 503 {
		t.Fatal("DB outage reported ready")
	}
	timeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	failedStore, openErr := Open(timeout, databaseURL)
	cancel()
	if failedStore != nil || openErr == nil {
		t.Fatal("API storage initialization succeeded while DB was stopped")
	}
	docker(t, nil, "start", container)
	ready := false
	for start := time.Now(); time.Since(start) < 20*time.Second; time.Sleep(100 * time.Millisecond) {
		if pool.Ping(ctx) == nil {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("fixture PostgreSQL did not restart")
	}
	fixture.stop()
	fixture = startFixture(t, databaseURL)
	if replay := mutate(t, fixture, "actor-a", lost); !reflect.DeepEqual(replay, lostResult) {
		t.Fatal("DB/API restart lost committed receipt")
	}
	if count("SELECT count(*) FROM shared_receipts WHERE operation_id='op-outage'") != 0 {
		t.Fatal("outage persisted a successful operation")
	}
	restoredURL := newDatabase("koujou_restored")
	docker(t, backup, "exec", "-i", container, "pg_restore", "-U", "postgres", "--exit-on-error", "-d", "koujou_restored")
	restored := startFixture(t, restoredURL)
	if replay := mutate(t, restored, "actor-a", lost); !reflect.DeepEqual(replay, lostResult) {
		t.Fatal("backup restore changed committed result/version/position")
	}
	restoredPool := testPool(t, restoredURL)
	var restoredVersion string
	var restoredPayload json.RawMessage
	if err = restoredPool.QueryRow(ctx, "SELECT version,payload FROM fixture_records WHERE workspace_id='workspace-a' AND id='resource-a'").Scan(&restoredVersion, &restoredPayload); err != nil || restoredVersion != lostResult.Data.Version || !bytes.Equal(restoredPayload, raw) {
		t.Fatal("backup restore changed domain data or its version")
	}
	var restoredPosition string
	if err = restoredPool.QueryRow(ctx, "SELECT position FROM shared_streams WHERE workspace_id='workspace-feed'").Scan(&restoredPosition); err != nil || restoredPosition != *feed4.Meta.Position {
		t.Fatal("backup restore changed captured update position")
	}
	forked := mutate(t, restored, "actor-f", command("workspace-feed", "restored-fork", "feed-fork", "0", "restored-fork"))
	if forked.Data == nil || *forked.Meta.Position == *postBackup.Meta.Position {
		t.Fatal("restored branch reused an old event position")
	}
	missing := page(restored.url, *postBackup.Meta.Position)
	if missing.Data != nil || missing.Error == nil || missing.Meta.Completeness.State != "pending" || missing.Meta.Completeness.Reason != "history_unavailable" {
		t.Fatal("missing restored history was treated as complete")
	}
	if _, err = restoredPool.Exec(ctx, "DELETE FROM shared_changes WHERE workspace_id='workspace-feed' AND sequence=2"); err != nil {
		t.Fatal(err)
	}
	gap := page(restored.url, *feed1.Meta.Position)
	if gap.Data != nil || gap.Error == nil || gap.Meta.Completeness.Reason != "history_unavailable" {
		t.Fatal("history gap was silently skipped")
	}
	t.Log("AC-6: isolated DB stop/restart and pg_dump/pg_restore preserved data, versions, positions and receipts")
	t.Log("AC-4: missing restored position and journal gap require snapshot instead of false completeness")
}
