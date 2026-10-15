package searchindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

type processUsage struct{ PeakRSS, ReadBytes, WriteBytes uint64 }

var measureProcess func() (processUsage, error)

// seedLoad writes a deterministic, internally consistent projection fixture in
// bulk. Functional tests exercise normal writes; this isolates search workload.
func seedLoad(ctx context.Context, store *persistence.Store) error {
	db := store.Database()
	_, err := db.Exec(ctx, `INSERT INTO auth_users VALUES('load-user','load-fixture','load-user','load@koujou.test','load user',false);
 INSERT INTO access_workspaces VALUES('load','load','load-user');
 INSERT INTO access_members(workspace_id,user_id,role) VALUES('load','load-user','admin');
 INSERT INTO shared_streams VALUES('load','load-origin','load-origin',0);`)
	if err != nil {
		return err
	}
	records, versions, changes, resources := [][]any{}, [][]any{}, [][]any{}, [][]any{}
	var sequence int64
	add := func(resource, kind, version string, payload any) {
		sequence++
		raw := encode(payload)
		position := fmt.Sprintf("load-position-%d", sequence)
		records = append(records, []any{"load", resource, kind, version, json.RawMessage(raw), sequence})
		changes = append(changes, []any{"load", sequence, position, resource, version, "context-" + kind, false, json.RawMessage(encode(contexts.Record{Ref: contexts.Ref{Kind: kind, ID: strings.SplitN(resource, ":", 2)[1], Revision: version}, Value: raw}))})
	}
	scope := func(n int) contexts.Identity {
		return contexts.Identity{Kind: "issue", ID: fmt.Sprintf("scope-%03d", n)}
	}
	for i := 0; i < 100; i++ {
		id := scope(i).ID
		add("scope:"+id, "scope", "scope-version-"+id, contexts.Scope{Kind: "issue", Name: id})
		versions = append(versions, []any{"load", "scope:" + id, "scope-version-" + id, false})
		resources = append(resources, []any{"load", "scope:" + id, false, nil})
	}
	cases := qualityCases()
	for family := 0; family < 2500; family++ {
		prefix := fmt.Sprintf("load%05d", family)
		if family < 40 {
			prefix = fmt.Sprintf("q%02d", family)
		}
		for revision := 0; revision < 3; revision++ {
			var child contexts.Ref
			for _, kind := range []string{"sentence", "paragraph", "section", "document"} {
				id := prefix + "-" + kind
				version := fmt.Sprintf("%s-r%d", id, revision)
				unit := contexts.Unit{Kind: kind, Name: id, Tags: []string{"load", fmt.Sprintf("group-%03d", family%100)}}
				if kind == "sentence" {
					text := fmt.Sprintf("一般の作業記録と進捗。routine work item %d createOtherRun read_other_context", family)
					if family < 40 {
						text = cases[family].text
					}
					unit.Text = &text
				} else {
					unit.Children = []contexts.Ref{child}
				}
				add("context:"+id, kind, version, unit)
				child = contexts.Ref{Kind: kind, ID: id, Revision: version}
				if revision == 2 {
					versions = append(versions, []any{"load", "context:" + id, version, false})
					resources = append(resources, []any{"load", "context:" + id, false, nil})
				}
			}
		}
		id := prefix + "-membership"
		for revision := 0; revision < 3; revision++ {
			version := fmt.Sprintf("%s-r%d", id, revision)
			add("membership:"+id, "membership", version, contexts.Membership{Scope: scope(family % 100), Context: contexts.Identity{Kind: "document", ID: prefix + "-document"}, Present: revision != 1})
			if revision == 2 {
				versions = append(versions, []any{"load", "membership:" + id, version, false})
				resources = append(resources, []any{"load", "membership:" + id, false, nil})
			}
		}
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, table := range []struct {
			name    string
			columns []string
			rows    [][]any
		}{
			{"context_records", []string{"workspace_id", "resource_id", "kind", "revision", "payload", "stream_sequence"}, records},
			{"shared_versions", []string{"workspace_id", "resource_id", "version", "deleted"}, versions},
			{"shared_changes", []string{"workspace_id", "sequence", "position", "resource_id", "version", "kind", "deleted", "payload"}, changes},
			{"access_resources", []string{"workspace_id", "id", "restricted", "assigned_device"}, resources},
		} {
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{table.name}, table.columns, pgx.CopyFromRows(table.rows)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, "UPDATE shared_streams SET sequence=$1,position=$2 WHERE workspace_id='load'", sequence, fmt.Sprintf("load-position-%d", sequence))
		return err
	})
}

func TestSearchLoad(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:search-load")
	}
	if measureProcess == nil {
		t.Skip("agreed process RSS/I/O measurement runs on the recorded Windows host")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = seedLoad(ctx, store); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("b", 32))
	a := access.New(store, key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
		if r.Header.Get("Authorization") != "Bearer load-user" {
			w.WriteHeader(401)
			return "", false
		}
		return "load-user", true
	})
	c := contexts.New(store, a, contexts.DefaultLimits())
	s := New(store, a, c, t.TempDir(), key)
	defer s.Close()
	initial, err := measureProcess()
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if err = s.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	rebuild := time.Since(began)
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	search := func(r Request) (Result, error) {
		req, _ := http.NewRequest("POST", server.URL+"/search/workspaces/load/query", bytes.NewReader(encode(r)))
		req.Header.Set("Authorization", "Bearer load-user")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return Result{}, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return Result{}, err
		}
		var result apicontract.Response[Result]
		if json.Unmarshal(raw, &result) != nil || resp.StatusCode != 200 || result.Data == nil || result.Meta.Completeness.State != "complete" {
			return Result{}, fmt.Errorf("search failed: %d %s", resp.StatusCode, raw)
		}
		return *result.Data, nil
	}
	cases := qualityCases()
	for _, example := range cases {
		result, err := search(example.request)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range result.Items {
			found = found || item.Ref.ID == example.id
		}
		if !found {
			t.Fatalf("quality failed: %s", example.word)
		}
	}
	// Forty quality requests double as warmup; then four concurrent clients each
	// execute all forty queries, including HTTP/JSON and current SoT authorization.
	durations := make(chan time.Duration, 160)
	failures := make(chan error, 4)
	var clients sync.WaitGroup
	began = time.Now()
	for client := 0; client < 4; client++ {
		clients.Add(1)
		go func() {
			defer clients.Done()
			for _, example := range cases {
				started := time.Now()
				if _, err := search(example.request); err != nil {
					failures <- err
					return
				}
				durations <- time.Since(started)
			}
		}()
	}
	clients.Wait()
	elapsed := time.Since(began)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	close(durations)
	samples := []float64{}
	for d := range durations {
		samples = append(samples, float64(d.Microseconds())/1000)
	}
	sort.Float64s(samples)
	if len(samples) != 160 {
		t.Fatal("incomplete load")
	}
	p95 := samples[(len(samples)*95+99)/100-1]
	s.Start(ctx, false)
	var oldVersion string
	if err = store.Database().QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='load' AND resource_id='context:q00-sentence'").Scan(&oldVersion); err != nil {
		t.Fatal(err)
	}
	changed := "遅延計測の新しい本文"
	saved, err := c.Put(ctx, access.Principal{UserID: "load-user"}, "load", contexts.Identity{Kind: "sentence", ID: "q00-sentence"}, contexts.Write{Mutation: apicontract.Mutation{OperationID: "load-update", ExpectedVersion: oldVersion}, Value: encode(contexts.Unit{Kind: "sentence", Text: &changed})})
	if err != nil || saved.Data == nil {
		t.Fatalf("load update: %v", err)
	}
	began = time.Now()
	observed := false
	for time.Since(began) < 5*time.Second {
		result, err := search(Request{IDs: []string{"q00-sentence"}, Limit: 10})
		if err == nil && result.Count == 1 && result.Items[0].Ref.Revision == saved.Data.Version {
			observed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	lag := time.Since(began)
	if !observed {
		t.Fatal("reflection lag exceeds 5s")
	}
	usage, err := measureProcess()
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"schema": schema, "os": runtime.GOOS, "go": runtime.Version(), "contexts": 10000, "revisions_per_context": 3, "scopes": 100, "membership_revisions": 7500, "quality_passed": 40, "quality_total": 40, "concurrency": 4, "warmup": 40, "requests": len(samples), "p95_ms": p95, "load_elapsed_ms": elapsed.Milliseconds(), "requests_per_second": 160 / elapsed.Seconds(), "rebuild_ms": rebuild.Milliseconds(), "reflection_lag_ms": lag.Milliseconds(), "peak_process_rss_bytes": usage.PeakRSS, "process_read_transfer_bytes": usage.ReadBytes - initial.ReadBytes, "process_write_transfer_bytes": usage.WriteBytes - initial.WriteBytes}
	raw, _ := json.MarshalIndent(report, "", "  ")
	t.Log("SEARCH_LOAD_METRICS " + string(raw))
	if path := os.Getenv("KOUJOU_SEARCH_LOAD_REPORT"); path != "" {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if p95 > 500 || rebuild > 120*time.Second || usage.PeakRSS > 1<<30 {
		t.Fatal("agreed search performance target not met")
	}
}
