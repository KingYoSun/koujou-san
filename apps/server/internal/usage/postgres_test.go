package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func TestUsagePostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:usage")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := store.Database().Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, id := range []string{"admin", "editor", "viewer", "outsider"} {
		exec(`INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)`, id)
	}
	exec(`INSERT INTO access_workspaces VALUES('one','one','admin'),('two','two','outsider'); INSERT INTO access_members(workspace_id,user_id,role) VALUES('one','admin','admin'),('one','editor','editor'),('one','viewer','viewer'),('two','outsider','admin'); INSERT INTO access_resources VALUES('one','scope:a',false,NULL),('one','scope:b',false,NULL),('one','run:case',false,NULL)`)
	a := access.New(store, []byte(strings.Repeat("k", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := r.Header.Get("X-Test-User")
		if id == "" {
			w.WriteHeader(401)
			return "", false
		}
		return id, true
	})
	s := New(store, a)
	mux := http.NewServeMux()
	s.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(actor, method, path string, body any, want int, out any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		r, _ := http.NewRequest(method, server.URL+"/usage/workspaces/one/"+path, bytes.NewReader(raw))
		r.Header.Set("X-Test-User", actor)
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var value json.RawMessage
		if e = json.NewDecoder(res.Body).Decode(&value); e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d %s", method, path, res.StatusCode, want, value)
		}
		if out != nil {
			if e = json.Unmarshal(value, out); e != nil {
				t.Fatal(e)
			}
		}
	}
	serial := 0
	write := func(id, expected string, v any) Write {
		serial++
		raw, _ := json.Marshal(v)
		return Write{Mutation: apicontract.Mutation{OperationID: fmt.Sprint("write-", serial), ExpectedVersion: expected}, Value: raw}
	}
	put := func(id, expected string, v any) Ref {
		t.Helper()
		var response apicontract.Response[apicontract.Reference]
		request("editor", "POST", "records/"+id, write(id, expected, v), 200, &response)
		if response.Data == nil {
			t.Fatal(response)
		}
		return Ref{ID: id, Version: response.Data.Version}
	}
	query := func(actor string, r Request) apicontract.Response[Page] {
		t.Helper()
		var out apicontract.Response[Page]
		if _, e := s.Query(ctx, access.Principal{UserID: actor}, "one", r); e != nil {
			t.Fatalf("query %s/%s: %v", r.Mode, r.Kind, e)
		}
		request(actor, "POST", "query", r, 200, &out)
		if out.Data == nil {
			t.Fatal(out)
		}
		return out
	}
	period := Request{Kind: "usage", From: tariff().From, Until: tariff().From.AddDate(1, 0, 0), Timezone: "Asia/Tokyo", Limit: 100}
	o := observation("first", "req", "cli", "100")
	o.Quantities = quantities()
	o.Attribution.Resource = "run:case"
	id := ObservationID(o)
	req := write(id, "0", o)
	var first, replay apicontract.Response[apicontract.Reference]
	request("editor", "POST", "records/"+id, req, 200, &first)
	request("editor", "POST", "records/"+id, req, 200, &replay)
	if first.Data == nil || replay.Data == nil || *first.Data != *replay.Data {
		t.Fatal("replay changed", first, replay)
	}
	price := put("price:individual", "0", tariff())
	e := Evaluation{Group: o.Group, Kind: "estimated", Price: &price}
	eval := put("evaluation:first", "0", e)
	var saved Record
	request("viewer", "GET", "records/"+eval.ID+"?version="+eval.Version, nil, 200, &saved)
	var ev Evaluation
	_ = json.Unmarshal(saved.Value, &ev)
	if ev.Amount == nil || *ev.Amount != "0.182000" || ev.Price == nil || *ev.Price != price || len(ev.Adopted) != 4 {
		t.Fatal(string(saved.Value))
	}
	for _, kind := range []string{"reported", "api_equivalent", "allocated"} {
		v := e
		v.Kind = kind
		if kind != "api_equivalent" {
			v.Price = nil
			v.Amount = ptr("0.100000")
			v.Source = "fictional report"
			v.Currency = "USD"
			v.Rounding = "half_up"
			v.Scale = 6
		}
		put("evaluation:"+kind, "0", v)
	}
	for _, kind := range []string{"estimated", "reported", "api_equivalent", "allocated"} {
		r := period
		r.Kind = kind
		page := query("viewer", r)
		if len(page.Data.Items) != 1 {
			t.Fatal(kind, page)
		}
		t.Log(kind, string(page.Data.Items[0]))
	}
	p2 := tariff()
	p2.Rates["input"] = Rate{Amount: "0.002", Per: "1", Unit: "token"}
	price2 := put(price.ID, price.Version, p2)
	request("viewer", "GET", "records/"+eval.ID+"?version="+eval.Version, nil, 200, &saved)
	_ = json.Unmarshal(saved.Value, &ev)
	if *ev.Amount != "0.182000" || *ev.Price != price {
		t.Fatal("rewrote old evaluation")
	}
	e.Price = &price2
	e.Previous = &eval
	put("evaluation:revalued", "0", e)
	// A correction changes the current aggregate basis and preserves old history.
	o.Quantities["image"] = Quantity{Value: ptr("2"), State: "reported", Unit: "image"}
	corrected := put(id, first.Data.Version, o)
	r := period
	r.Kind = "estimated"
	page := query("viewer", r)
	if !strings.Contains(string(page.Data.Items[0]), `"incomplete":1`) || !strings.Contains(string(page.Data.Items[0]), `"amount":null`) {
		t.Fatal("stale cost", page)
	}
	e.Price = &price
	e.Previous = &eval
	p := tariff()
	p.Rates["image"] = Rate{Amount: "0.03", Per: "1", Unit: "image"}
	imagePrice := put("price:image", "0", p)
	e.Price = &imagePrice
	ev2 := put("evaluation:corrected", "0", e)
	e.FX = &Exchange{Rate: "150", From: "USD", To: "JPY", At: o.At, Source: "fictional FX fixture"}
	put("evaluation:converted", "0", e)
	request("viewer", "GET", "records/evaluation:converted", nil, 200, &saved)
	_ = json.Unmarshal(saved.Value, &ev)
	if *ev.Amount != "36.300000" || ev.Currency != "JPY" {
		t.Fatal(string(saved.Value))
	}
	r = period
	r.Kind = "estimated"
	page = query("viewer", r)
	if len(page.Data.Items) != 2 {
		t.Fatal("currencies merged", page)
	}
	// Both overlapping Scopes select the same consumption once.
	r = period
	r.Scopes = []string{"a", "b"}
	page = query("viewer", r)
	if len(page.Data.Items) != 1 || !strings.Contains(string(page.Data.Items[0]), `"records":1`) {
		t.Fatal(page)
	}
	// Changing document grouping does not change explicit cost affiliations.
	beforeGrouping := string(page.Data.Items[0])
	contextService := contexts.New(store, a, contexts.DefaultLimits())
	contextPut := func(kind, id, expected string, value any) string {
		t.Helper()
		raw, _ := json.Marshal(value)
		serial++
		saved, e := contextService.Put(ctx, access.Principal{UserID: "editor"}, "one", contexts.Identity{Kind: kind, ID: id}, contexts.Write{Mutation: apicontract.Mutation{OperationID: fmt.Sprint("grouping-", serial), ExpectedVersion: expected}, Value: raw})
		if e != nil || saved.Data == nil {
			t.Fatal(saved, e)
		}
		return saved.Data.Version
	}
	contextPut("scope", "b", "0", contexts.Scope{Kind: "issue", Name: "grouping fixture"})
	text := "Grouping does not allocate cost"
	contextPut("sentence", "cost-grouping", "0", contexts.Unit{Kind: "sentence", Text: &text})
	membership := contexts.Membership{Scope: contexts.Identity{Kind: "issue", ID: "b"}, Context: contexts.Identity{Kind: "sentence", ID: "cost-grouping"}, Present: true}
	membershipVersion := contextPut("membership", "cost-grouping", "0", membership)
	membership.Present = false
	contextPut("membership", "cost-grouping", membershipVersion, membership)
	if after := query("viewer", r); len(after.Data.Items) != 1 || string(after.Data.Items[0]) != beforeGrouping {
		t.Fatal("Context grouping changed cost affiliation", after)
	}
	// Current case and tariff policies protect record, aggregate and export equally.
	setPrivate := func(id string) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
			return a.PutResource(ctx, tx, access.Resource{WorkspaceID: "one", ID: id, Restricted: true}, []access.ResourceGrant{{UserID: "admin", Read: true, Update: true}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	setPrivate(imagePrice.ID)
	request("viewer", "GET", "records/"+imagePrice.ID, nil, 403, nil)
	request("viewer", "GET", "records/"+ev2.ID, nil, 403, nil)
	r = period
	r.Kind = "estimated"
	if len(query("viewer", r).Data.Items) != 0 {
		t.Fatal("private amount in aggregate")
	}
	r.Mode = "export"
	if len(query("viewer", r).Data.Items) != 0 {
		t.Fatal("private amount in export")
	}
	setPrivate("run:case")
	request("viewer", "GET", "records/"+corrected.ID, nil, 403, nil)
	r = period
	if len(query("viewer", r).Data.Items) != 0 {
		t.Fatal("private case in totals")
	}
	request("outsider", "POST", "query", period, 403, nil)
	request("viewer", "POST", "records/price:denied", write("", "0", tariff()), 403, nil)
	exec(`UPDATE access_members SET active=false WHERE workspace_id='one' AND user_id='editor'`)
	request("editor", "POST", "records/"+id, req, 403, nil)
	// Current operation boundaries also apply inside SQL aggregates.
	reader := access.Principal{UserID: "admin"}
	op, e1 := a.CreateOperation(ctx, reader, "one", "none", "read", true, nil, apicontract.Mutation{OperationID: "none", ExpectedVersion: "0"})
	if e1 != nil || op.Error != nil {
		t.Fatal(op, e1)
	}
	reader.OperationID = "none"
	none, err := s.Query(ctx, reader, "one", period)
	if err != nil || len(none.Data.Items) != 0 {
		t.Fatal("none broadened", none, err)
	}
	// Read-only queries did not publish, execute, or add evaluations.
	var before, after int
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM usage_records").Scan(&before)
	query("admin", period)
	_ = store.Database().QueryRow(ctx, "SELECT count(*) FROM usage_records").Scan(&after)
	if before != after {
		t.Fatal("read wrote domain records")
	}
	changes := query("admin", Request{Mode: "changes", Kind: "usage", Limit: 1})
	if len(changes.Data.Items) != 1 || changes.Meta.NextCursor == nil {
		t.Fatal(changes)
	}
	next := Request{Mode: "changes", Kind: "usage", Limit: 1, Cursor: *changes.Meta.NextCursor}
	second := query("admin", next)
	if second.Data.Through != changes.Data.Through || !second.Data.AsOf.Equal(changes.Data.AsOf) {
		t.Fatal("page cutoff changed")
	}
	t.Run("attributions_and_missing", func(t *testing.T) {
		exec(`UPDATE access_members SET active=true WHERE workspace_id='one' AND user_id='editor'`)
		for i, kind := range []string{"run", "operation", "hopper", "triage", "unattributed"} {
			o := observation(fmt.Sprint("kind-", i), fmt.Sprint("kind-request-", i), "cli", "0")
			o.Group = fmt.Sprint("kind-group-", i)
			o.Attribution = Attribution{Kind: kind, Scopes: []string{"a", "b"}, Workflow: "wf"}
			if kind != "unattributed" {
				o.Attribution.ID = fmt.Sprint("owner-", i)
			}
			o.ActualModel = nil
			o.Quantities["missing"] = Quantity{State: "missing", Unit: "image"}
			o.Quantities["na"] = Quantity{State: "not_applicable", Unit: "second"}
			ref := put(ObservationID(o), "0", o)
			var raw Record
			request("viewer", "GET", "records/"+ref.ID, nil, 200, &raw)
			var got Observation
			_ = json.Unmarshal(raw.Value, &got)
			var stored json.RawMessage
			if err := store.Database().QueryRow(ctx, `SELECT payload FROM usage_records WHERE workspace_id='one' AND id=$1 AND version=$2`, ref.ID, ref.Version).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			var httpValue, dbValue any
			_ = json.Unmarshal(raw.Value, &httpValue)
			_ = json.Unmarshal(stored, &dbValue)
			if !reflect.DeepEqual(httpValue, dbValue) {
				t.Fatal("HTTP/PG original record differs")
			}
			if got.ActualModel != nil || got.Quantities["input"].Value == nil || *got.Quantities["input"].Value != "0" || got.Quantities["missing"].Value != nil || got.Quantities["missing"].State != "missing" || got.Quantities["na"].State != "not_applicable" {
				t.Fatal(string(raw.Value))
			}
			r := period
			r.Attribution = kind
			r.Workflow = "wf"
			page := query("viewer", r)
			if len(page.Data.Items) != 1 || !strings.Contains(string(page.Data.Items[0]), `"records":1`) {
				t.Fatal("attribution/scope distinctness", page)
			}
			put(fmt.Sprint("evaluation:unpriced-", i), "0", Evaluation{Group: o.Group, Kind: "estimated"})
		}
	})
	t.Run("large_decimal_through_http_and_pg", func(t *testing.T) {
		o := observation("large", "large", "cli", "12000000000000001")
		o.Group = "large"
		put(ObservationID(o), "0", o)
		price := tariff()
		price.Scale = 9
		price.Rates = map[string]Rate{"input": {Amount: "0.000000001", Per: "1", Unit: "token"}}
		priceRef := put("price:large", "0", price)
		ref := put("evaluation:large", "0", Evaluation{Group: o.Group, Kind: "estimated", Price: &priceRef})
		var r Record
		request("viewer", "GET", "records/"+ref.ID, nil, 200, &r)
		var e Evaluation
		_ = json.Unmarshal(r.Value, &e)
		var stored string
		if err := store.Database().QueryRow(ctx, `SELECT payload->>'amount' FROM usage_records WHERE workspace_id='one' AND id=$1 AND version=$2`, ref.ID, ref.Version).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if e.Amount == nil || *e.Amount != "12000000.000000001" || stored != *e.Amount {
			t.Fatal("decimal precision was lost", stored, e.Amount)
		}
	})
	t.Run("cumulative_and_late_children", func(t *testing.T) {
		o := observation("cum-1", "cum", "cli", "100")
		o.Mode = "cumulative"
		o.Group = "cum"
		o.Series = "session"
		o.Sequence = 1
		ref := put(ObservationID(o), "0", o)
		o.Event = "cum-2"
		o.Sequence = 2
		q := o.Quantities["input"]
		q.Value = ptr("150")
		o.Quantities["input"] = q
		put(ref.ID, ref.Version, o)
		r := period
		r.Mode = "export"
		page := query("admin", r)
		var found bool
		for _, raw := range page.Data.Items {
			if strings.Contains(string(raw), `"group":"cum"`) {
				if !strings.Contains(string(raw), `"value":"150"`) {
					t.Fatal(string(raw))
				}
				found = true
			}
		}
		if !found {
			t.Fatal("cumulative group missing")
		}
		parent := observation("parent", "parent", "cli", "200")
		parent.Group = "family"
		parent.Covers = []string{"child-a", "child-b"}
		put(ObservationID(parent), "0", parent)
		for i, value := range []string{"70", "60"} {
			child := observation(fmt.Sprint("child-", i), []string{"child-a", "child-b"}[i], "cli", value)
			child.Group = "family"
			put(ObservationID(child), "0", child)
		}
		gateway := parent
		gateway.Source = "gateway"
		gateway.Event = "gateway"
		put(ObservationID(gateway), "0", gateway)
		page = query("admin", r)
		found = false
		for _, raw := range page.Data.Items {
			if strings.Contains(string(raw), `"group":"family"`) {
				if !strings.Contains(string(raw), `"value":"200"`) || !strings.Contains(string(raw), `"state":"confirmed"`) {
					t.Fatal(string(raw))
				}
				found = true
			}
		}
		if !found {
			t.Fatal("parent group missing")
		}
	})
	t.Run("ten_thousand_three_versions", func(t *testing.T) {
		exec(`INSERT INTO access_workspaces VALUES('load','load','admin'); INSERT INTO access_members(workspace_id,user_id,role) VALUES('load','admin','admin'); INSERT INTO shared_streams(workspace_id,origin,position,sequence) VALUES('load','load-origin','load-final',30000)`)
		exec(`INSERT INTO access_resources SELECT 'load','scope:'||i,false,NULL FROM generate_series(0,99) i;
 INSERT INTO access_resources SELECT 'load','usage:load-'||i,false,NULL FROM generate_series(1,10000) i;
 INSERT INTO usage_records(workspace_id,id,version,kind,position,coverage_group,payload)
 SELECT 'load','usage:load-'||i,'v'||v,'usage',(i-1)*3+v,'group-'||i,jsonb_build_object('source','fixture','sequence',v,'at','2026-01-01T00:00:00Z') FROM generate_series(1,10000) i CROSS JOIN generate_series(1,3) v;
 INSERT INTO usage_entries
 SELECT 'load','group-'||i||':usage:',(i-1)*3+v,'group-'||i,'usage','','2026-01-01T00:00:00Z'::timestamptz,
 (ARRAY['run','operation','hopper','triage','unattributed'])[(i%5)+1],CASE WHEN i%5=4 THEN '' ELSE 'owner-'||i END,'workflow-'||(i%10),ARRAY[(i%100)::text,((i+1)%100)::text],'fixture-model',NULL,'confirmed',
 jsonb_build_array(jsonb_build_object('id','usage:load-'||i,'version','v'||v),jsonb_build_object('id','scope:'||(i%100),'version',''),jsonb_build_object('id','scope:'||((i+1)%100),'version','')),
 '{}'::jsonb,jsonb_build_object('input',jsonb_build_object('state','reported','unit','token','value',v::text))
 FROM generate_series(1,10000) i CROSS JOIN generate_series(1,3) v;
 ANALYZE usage_records; ANALYZE usage_entries; ANALYZE access_resources`)
		r := period
		r.Limit = 2
		r.Attribution = "run"
		r.Scopes = []string{"0", "1"}
		start := time.Now()
		page, err := s.Query(ctx, access.Principal{UserID: "admin"}, "load", r)
		if err != nil || page.Data == nil || len(page.Data.Items) != 2 || page.Meta.NextCursor == nil {
			t.Fatal(page, err)
		}
		elapsed := time.Since(start)
		r.Cursor = *page.Meta.NextCursor
		// A later source version is outside the fixed page cutoff.
		exec(`UPDATE shared_streams SET sequence=30001,position='later' WHERE workspace_id='load'`)
		next, err := s.Query(ctx, access.Principal{UserID: "admin"}, "load", r)
		if err != nil || next.Data.Through != 30000 || next.Data.AsOf != page.Data.AsOf {
			t.Fatal(next, err)
		}
		r.Cursor = ""
		r.Mode = "records"
		r.Kind = "usage"
		r.Limit = 2
		history, err := s.Query(ctx, access.Principal{UserID: "admin"}, "load", r)
		if err != nil || len(history.Data.Items) != 2 || history.Meta.NextCursor == nil {
			t.Fatal(history, err)
		}
		var plan string
		if err = store.Database().QueryRow(ctx, `EXPLAIN SELECT * FROM usage_records WHERE workspace_id='load' AND coverage_group='group-1' AND kind='usage' ORDER BY id,position DESC`).Scan(&plan); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, "Index") && !strings.Contains(plan, "Bitmap") {
			t.Fatal("group lookup did not use an index", plan)
		}
		t.Logf("10,000 records x 3 versions, 100 Scopes; bounded aggregate page=%d, first page=%s, group plan=%s", len(page.Data.Items), elapsed, plan)
	})
}
