package graphindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func fixtureDocker(t *testing.T, args ...string) []byte {
	t.Helper()
	result, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("owned graph fixture operation: %v %s", err, result)
	}
	return result
}
func graphContainer(t *testing.T) string {
	t.Helper()
	container, project := os.Getenv("KOUJOU_TEST_GRAPH_CONTAINER"), os.Getenv("KOUJOU_TEST_PROJECT")
	if container == "" || !strings.HasPrefix(project, "koujou-san-db-test-") {
		t.Fatal("isolated graph fixture required")
	}
	label := strings.TrimSpace(string(fixtureDocker(t, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project"}}`, container)))
	if label != project {
		t.Fatal("refusing another graph container")
	}
	return container
}
func seedGraphLoad(ctx context.Context, store *persistence.Store) error {
	db := store.Database()
	_, err := db.Exec(ctx, `INSERT INTO auth_users VALUES('load-user','graph-load','load-user','load@koujou.test','load user',false);
 INSERT INTO access_workspaces VALUES('load','load','load-user');INSERT INTO access_members(workspace_id,user_id,role) VALUES('load','load-user','admin');INSERT INTO shared_streams VALUES('load','load-origin','load-origin',0);`)
	if err != nil {
		return err
	}
	records, relations, versions, resources, changes := [][]any{}, [][]any{}, [][]any{}, [][]any{}, [][]any{}
	sequence := int64(0)
	add := func(id, kind, revision string, value any, record bool) {
		sequence++
		payload := json.RawMessage(encode(value))
		if record {
			records = append(records, []any{"load", id, kind, revision, payload, sequence})
		} else if kind == "relation" {
			relations = append(relations, []any{"load", strings.TrimPrefix(id, "relation:"), revision, payload, sequence})
		}
		changes = append(changes, []any{"load", sequence, fmt.Sprintf("graph-load-%d", sequence), id, revision, kind, false, payload})
	}
	current := func(id, version string) {
		versions = append(versions, []any{"load", id, version, false})
		resources = append(resources, []any{"load", id, false, nil})
	}
	ref := func(i int) contexts.Ref {
		return contexts.Ref{Kind: "sentence", ID: fmt.Sprintf("c%05d", i), Revision: fmt.Sprintf("c%05d-r2", i)}
	}
	for i := 0; i < 10000; i++ {
		r := ref(i)
		for v := 0; v < 3; v++ {
			revision := fmt.Sprintf("c%05d-r%d", i, v)
			text := fmt.Sprintf("graph load context %d version %d", i, v)
			add(resource(r), r.Kind, revision, contexts.Unit{Kind: r.Kind, Text: &text}, true)
		}
		current(resource(r), r.Revision)
	}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("scope:s%03d", i)
		add(id, "scope", "r0", contexts.Scope{Kind: "issue", Name: id}, true)
		current(id, "r0")
	}
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("run:r%04d", i)
		add(id, "run", "r0", map[string]string{"kind": "declared-load-fixture"}, false)
		current(id, "r0")
	}
	// Exactly 200,000 source relationships: a 5,000-parent shared Sentence,
	// a depth-8 chain with 20-way branches, Run use/generation, and semantic links.
	for i := 0; i < 200000; i++ {
		r := Relation{Kind: "semantic", From: ref(1000 + (i/20)%9000), To: ref(1000 + (i*97+31)%9000), Order: i % 20, Present: true, Origin: &contexts.Origin{Kind: "adopted-fixture", ID: "proposal", Revision: "fixed-1", Digest: strings.Repeat("b", 64)}}
		switch {
		case i < 5000:
			r.Kind = "reference"
			r.From = ref(i + 1)
			r.To = ref(0)
		case i < 5180:
			n := i - 5000
			r.Kind = "reference"
			r.From = ref(500 + n/20)
			r.To = ref(501 + n/20 + n%20)
		case i < 25180:
			r.Kind = []string{"uses", "generated"}[i%2]
			r.From = contexts.Ref{Kind: "run", ID: fmt.Sprintf("r%04d", i%1000), Revision: "r0"}
			r.To = ref(1000 + i%9000)
		}
		id := fmt.Sprintf("relation:e%06d", i)
		add(id, "relation", "r0", r, false)
		current(id, "r0")
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, table := range []struct {
			name    string
			columns []string
			values  [][]any
		}{{"context_records", []string{"workspace_id", "resource_id", "kind", "revision", "payload", "stream_sequence"}, records}, {"graph_relations", []string{"workspace_id", "id", "revision", "payload", "stream_sequence"}, relations}, {"shared_versions", []string{"workspace_id", "resource_id", "version", "deleted"}, versions}, {"access_resources", []string{"workspace_id", "id", "restricted", "assigned_device"}, resources}, {"shared_changes", []string{"workspace_id", "sequence", "position", "resource_id", "version", "kind", "deleted", "payload"}, changes}} {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{table.name}, table.columns, pgx.CopyFromRows(table.values)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, "UPDATE shared_streams SET sequence=$1,position=$2 WHERE workspace_id='load'", sequence, fmt.Sprintf("graph-load-%d", sequence))
		return err
	})
}
func TestGraphLoad(t *testing.T) {
	if os.Getenv("KOUJOU_TEST_DATABASE_URL") == "" {
		t.Skip("run pnpm run test:graph-load")
	}
	ctx := context.Background()
	container := graphContainer(t)
	store, err := persistence.Open(ctx, os.Getenv("KOUJOU_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Log("seeding 10,000 Context x 3 versions, 100 Scope, 1,000 declared Run, 200,000 typed source relations")
	if err = seedGraphLoad(ctx, store); err != nil {
		t.Fatal(err)
	}
	a := access.New(store, []byte(strings.Repeat("l", 32)), func(http.ResponseWriter, *http.Request) (string, bool) { return "load-user", true })
	c := contexts.New(store, a, contexts.DefaultLimits())
	credential := os.Getenv("KOUJOU_TEST_GRAPH_PASSWORD")
	remote, err := NewRemote(os.Getenv("KOUJOU_TEST_GRAPH_URL"), "root", credential, "koujou_graph_load")
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, a, c, remote, DefaultLimits(), []byte(strings.Repeat("l", 32)))
	defer s.Close()
	t.Log("seed complete; starting native rebuild")
	begin := time.Now()
	if err = s.Rebuild(ctx, "load"); err != nil {
		t.Fatalf("rebuild: %#v", err)
	}
	rebuild := time.Since(begin)
	t.Logf("rebuild: %s", rebuild)
	// Files exceed the explicitly configured 16 MiB page cache; no padded payload
	// is added just to manufacture a cache-out result.
	disk := strings.Fields(string(fixtureDocker(t, "exec", container, "du", "-sb", "/home/arcadedb/databases/koujou_graph_load")))
	size, _ := strconv.ParseInt(disk[0], 10, 64)
	if size <= 16*1024*1024 {
		t.Fatal("data did not exceed page cache")
	}
	diagnosticNodes := []vertex{}
	diagnosticEdges := []map[string]string{}
	for i := 0; i < 500; i++ {
		diagnosticNodes = append(diagnosticNodes, vertex{K: fmt.Sprint(i), Kind: "sentence", ID: fmt.Sprintf("c%05d", i), Rev: fmt.Sprintf("c%05d-r2", i)})
		diagnosticEdges = append(diagnosticEdges, map[string]string{"k": fmt.Sprint(i), "kind": "relation", "id": fmt.Sprintf("e%06d", i), "revision": "r0"})
	}
	for _, check := range []struct {
		label, sql string
		values     any
	}{{"node-source", nodeSourcesSQL, diagnosticNodes}, {"edge-source", edgeSourcesSQL, diagnosticEdges}} {
		rows, e := store.Database().Query(ctx, "EXPLAIN (ANALYZE,BUFFERS) "+check.sql, "load", encode(check.values))
		if e != nil {
			t.Fatal(e)
		}
		lines := []string{}
		for rows.Next() {
			var line string
			_ = rows.Scan(&line)
			lines = append(lines, line)
		}
		rows.Close()
		t.Logf("%s plan:\n%s", check.label, strings.Join(lines, "\n"))
	}
	mux := http.NewServeMux()
	s.Register(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	queryAPI := func(r Request) (apicontract.Response[Result], error) {
		response, err := api.Client().Post(api.URL+"/graph/workspaces/load/query", "application/json", bytes.NewReader(encode(r)))
		var result apicontract.Response[Result]
		if err != nil {
			return result, err
		}
		defer response.Body.Close()
		err = json.NewDecoder(response.Body).Decode(&result)
		if err == nil && response.StatusCode != 200 {
			err = fmt.Errorf("graph HTTP status %d", response.StatusCode)
		}
		return result, err
	}
	p := access.Principal{UserID: "load-user"}
	root := func(i int) contexts.Ref {
		return contexts.Ref{Kind: "sentence", ID: fmt.Sprintf("c%05d", i), Revision: fmt.Sprintf("c%05d-r2", i)}
	}
	cases := []Request{{Root: root(10), Kinds: []string{"version"}, Direction: "out"}, {Root: root(0), Kinds: []string{"reference"}, Direction: "in"}, {Root: root(500), Kinds: []string{"reference"}, Direction: "out"}, {Root: contexts.Ref{Kind: "run", ID: "r0001", Revision: "r0"}, Kinds: []string{"uses", "generated"}, Direction: "out"}}
	measure := func(rounds int, updates bool) ([]float64, time.Duration) {
		var mu sync.Mutex
		times := []float64{}
		byCase := map[int][]float64{}
		var maxLag time.Duration
		var wg sync.WaitGroup
		if updates {
			s.Start(ctx)
			defer func() { s.cancel(); s.workers.Wait() }()
			for worker := 0; worker < 4; worker++ {
				wg.Add(1)
				go func(worker int) {
					defer wg.Done()
					for i := 0; i < 10; i++ {
						id := fmt.Sprintf("load-update-%d-%d", worker, i)
						r := RelationWrite{OperationID: id, ExpectedVersion: "0", Value: Relation{Kind: "reference", From: root(9000 + worker), To: root(0), Present: true}}
						at := time.Now()
						result, err := s.PutRelation(ctx, p, "load", id, r)
						if err != nil || result.Data == nil {
							t.Errorf("load update: %v", err)
							return
						}
						for time.Since(at) < 6*time.Second {
							active, e := remote.Active(ctx, "load")
							h, e2 := headerRead(ctx, remote.DB, checkpointKey(active.Active))
							if e == nil && e2 == nil {
								cp, _ := parseCheckpoint(h)
								var seq int64
								_ = store.Database().QueryRow(ctx, "SELECT stream_sequence FROM graph_relations WHERE workspace_id='load' AND id=$1", id).Scan(&seq)
								if cp.Sequence >= seq {
									break
								}
							}
							time.Sleep(10 * time.Millisecond)
						}
						mu.Lock()
						maxLag = max(maxLag, time.Since(at))
						mu.Unlock()
					}
				}(worker)
			}
		}
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := 0; i < rounds; i++ {
					r := cases[(i+worker)%len(cases)]
					at := time.Now()
					result, err := queryAPI(r)
					duration := time.Since(at)
					if err != nil || result.Data == nil || result.Meta.Completeness.Reason == "timeout" || len(result.Data.Items) == 0 || result.Data.Items[0].Node == nil || result.Data.Items[0].Node.Ref() != r.Root {
						t.Errorf("load query failed: %v %+v", err, result.Meta)
					}
					mu.Lock()
					value := float64(duration.Microseconds()) / 1000
					times = append(times, value)
					byCase[(i+worker)%len(cases)] = append(byCase[(i+worker)%len(cases)], value)
					mu.Unlock()
				}
			}(worker)
		}
		wg.Wait()
		for k, values := range byCase {
			sort.Float64s(values)
			t.Logf("workload %d p95=%.1fms max=%.1fms", k, values[(len(values)*95-1)/100], values[len(values)-1])
		}
		sort.Float64s(times)
		return times, maxLag
	}
	// Warm up each workload before the fixed four-reader/four-writer run.
	for _, r := range cases {
		_, _ = queryAPI(r)
	}
	warm, lag := measure(20, true)
	usageBefore := string(fixtureDocker(t, "exec", container, "sh", "-c", "cat /sys/fs/cgroup/memory.peak /sys/fs/cgroup/io.stat"))
	// Restart the actual disposable container, retaining its real database/WAL.
	fixtureDocker(t, "restart", "--time", "3", container)
	ready := false
	for start := time.Now(); time.Since(start) < 40*time.Second; time.Sleep(200 * time.Millisecond) {
		if _, err = remote.Active(ctx, "load"); err == nil {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("graph restart not ready")
	}
	cold, _ := measure(10, false)
	peakRaw := strings.TrimSpace(string(fixtureDocker(t, "exec", container, "cat", "/sys/fs/cgroup/memory.peak")))
	peak, _ := strconv.ParseInt(peakRaw, 10, 64)
	beforePeak, _ := strconv.ParseInt(strings.Fields(usageBefore)[0], 10, 64)
	peak = max(peak, beforePeak)
	ioStats := string(fixtureDocker(t, "exec", container, "cat", "/sys/fs/cgroup/io.stat"))
	memoryLimit := strings.TrimSpace(string(fixtureDocker(t, "inspect", "--format", "{{.HostConfig.Memory}}", container)))
	flags := string(fixtureDocker(t, "exec", container, "sh", "-c", "jcmd $(cat /home/arcadedb/bin/arcadedb.pid) VM.flags"))
	p95 := func(values []float64) float64 { return values[(len(values)*95-1)/100] }
	report := map[string]any{"environment": runtime.GOOS + "/" + runtime.GOARCH, "server": "ArcadeDB 26.10.1", "driver": "Go HTTP 0.2.0", "context_count": 10000, "versions_per_context": 3, "scope_count": 100, "declared_run_count": 1000, "source_relations": 200000, "shared_sentence_parents": 5000, "depth": 8, "branches": 20, "readers": 4, "writers": 4, "page_cache_mib": 16, "wal_flush": 1, "container_limit_bytes": memoryLimit, "jvm_flags": flags, "database_bytes": size, "container_peak_bytes": peak, "io_stats": ioStats, "usage_before_restart": usageBefore, "rebuild_ms": rebuild.Milliseconds(), "warm_p95_ms": p95(warm), "restarted_p95_ms": p95(cold), "projection_lag_ms": lag.Milliseconds(), "warm_samples_ms": warm, "restart_samples_ms": cold}
	report["passed"] = !t.Failed() && rebuild <= 300*time.Second && p95(warm) <= 500 && p95(cold) <= 2000 && lag <= 5*time.Second && peak <= 1<<30
	raw, _ := json.MarshalIndent(report, "", "  ")
	path := os.Getenv("KOUJOU_GRAPH_LOAD_REPORT")
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0700)
		if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("load report: %s", bytes.TrimSpace(raw))
	if report["passed"] != true {
		t.Fatal("accepted graph load targets not met")
	}
}
