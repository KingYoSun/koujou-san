package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func checkComparison(t *testing.T, s *Service) {
	ctx := context.Background()
	p := access.Principal{UserID: "admin"}
	const w = "comparison"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Store.Database().Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO access_workspaces VALUES('comparison','Comparison fixture','admin'); INSERT INTO access_members(workspace_id,user_id,role) VALUES('comparison','admin','admin'),('comparison','editor','editor'),('comparison','viewer','viewer'); INSERT INTO access_resources SELECT 'comparison',id,false,NULL FROM unnest(ARRAY['scope:left','scope:right','run:compare-a','run:compare-b','context:compare-basis','run-definition:compare','inspection:compare','inspection-launch:compare','client-operation:compare','hopper-evaluation:compare','triage-evaluation:compare'])id`)
	serial := 0
	write := func(expected string, v any) Write {
		serial++
		b, _ := json.Marshal(v)
		return Write{Mutation: apicontract.Mutation{OperationID: fmt.Sprint("comparison-", serial), ExpectedVersion: expected}, Value: b}
	}
	put := func(id string, expected string, v any) Ref {
		t.Helper()
		r, err := s.Put(ctx, p, w, id, write(expected, v))
		if err != nil || r.Data == nil || r.Error != nil {
			t.Fatal(id, r.Error, err)
		}
		return Ref{ID: id, Version: r.Data.Version}
	}
	request := Request{Mode: "compare", Kind: "usage", From: tariff().From, Until: tariff().From.AddDate(1, 0, 0), Timezone: "Asia/Tokyo", Scopes: []string{"left", "right"}, Limit: 50}
	contextRef := Ref{ID: "context:compare-basis", Version: "context-v1"}
	definitionRef := Ref{ID: "run-definition:compare", Version: "definition-v1"}
	basis := []Ref{contextRef, definitionRef}
	// Fixed, owned read-model records; no process is launched by this fixture.
	for i, id := range []string{"run:compare-a", "run:compare-b"} {
		value := map[string]any{"state": "succeeded", "created_at": "2026-01-01T00:00:00Z", "started_at": "2026-01-01T00:01:00Z", "finished_at": "2026-01-01T00:03:00Z", "assessment": map[string]any{"state": "valid", "verdict": "PASS"}}
		if i == 1 {
			value["state"] = "failed"
			value["previous"] = map[string]any{"id": "run:compare-a", "version": "run-v1"}
			value["assessment"] = map[string]any{"state": "valid", "verdict": "FAIL"}
		}
		b, _ := json.Marshal(value)
		refs, _ := json.Marshal(basis)
		exec(`INSERT INTO run_records VALUES($1,$2,$2,'run-v1','run',0,$3,$4)`, w, id, b, refs)
	}
	exec(`INSERT INTO inspection_records VALUES('comparison','inspection-launch:compare','launch-v1','run:compare-b','launch',0,'{"definition":{"id":"inspection:compare","version":"criteria-v2"}}','[]'); INSERT INTO inspection_launches VALUES('comparison','run:compare-b','inspection-launch:compare','launch-v1')`)
	groups := []struct{ kind, id, amount, billing, state string }{{"run", "run:compare-a", "100", "api", "final"}, {"run", "run:compare-b", "80", "subscription", "partial"}, {"operation", "client-operation:compare", "0", "api", "final"}, {"hopper", "hopper-evaluation:compare", "5", "api", "final"}, {"triage", "triage-evaluation:compare", "7", "api", "final"}, {"unattributed", "", "0", "local", "not_applicable"}}
	observations := []Observation{}
	usageRefs := []Ref{}
	for i, g := range groups {
		o := observation(fmt.Sprint("compare-event-", i), fmt.Sprint("compare-request-", i), "owned-comparison", g.amount)
		o.At = request.From.Add(time.Hour)
		o.Attribution = Attribution{Kind: g.kind, ID: g.id, Resource: g.id, Scopes: []string{"left", "right"}}
		o.Group = fmt.Sprint("comparison-group-", i)
		o.Series = o.Group
		o.Measurement = &Measurement{State: g.state, Protocol: "owned-fixture", Billing: g.billing, Scope: "owned-main-session", Generation: "owned-generation"}
		if i == 0 {
			o.Quantities["cache"] = Quantity{Value: ptr("0"), State: "reported", Unit: "token"}
		}
		if i == 1 {
			o.ActualModel = ptr("actual-beta")
		}
		if i == 2 {
			o.Quantities = map[string]Quantity{"input": {State: "missing", Unit: "token", Reason: "owned_missing"}}
		}
		if i == 5 {
			o.Quantities = map[string]Quantity{"input": {State: "not_applicable", Unit: "token"}}
		}
		deps := []Ref{}
		if g.kind == "run" {
			deps = basis
		}
		r, err := s.PutFromOwner(ctx, p, w, ObservationID(o), write("0", o), OwnerWrite{Actor: "owned-comparison", Dependencies: deps, Check: func(context.Context, pgx.Tx) error { return nil }})
		if err != nil || r.Data == nil {
			t.Fatal(err, r.Error)
		}
		observations = append(observations, o)
		usageRefs = append(usageRefs, Ref{ID: r.Data.ID, Version: r.Data.Version})
	}
	query := func(actor string, r Request) apicontract.Response[Page] {
		t.Helper()
		page, err := s.Query(ctx, access.Principal{UserID: actor}, w, r)
		if err != nil || page.Data == nil {
			t.Fatal(r.Mode, err)
		}
		return page
	}
	page := query("admin", request)
	count := float64(0)
	for _, b := range page.Data.Items {
		var row map[string]any
		json.Unmarshal(b, &row)
		count += row["records"].(float64)
	}

	zeroSeen := false
	for _, b := range page.Data.Items {
		var row struct {
			Quantities []struct {
				Category          string
				Value             *string
				Reported, Missing int
			}
		}
		json.Unmarshal(b, &row)
		for _, q := range row.Quantities {
			if q.Category == "cache" && q.Value != nil && *q.Value == "0" && q.Reported == 1 && q.Missing == 0 {
				zeroSeen = true
			}
		}
	}
	if !zeroSeen {
		t.Fatal("reported zero was turned into missing or not applicable")
	}
	if count != 6 {
		t.Fatal("Scope overlap counted consumption twice", count)
	}
	var summary struct {
		Runs, Failed, Rework int
		Execution            string `json:"execution_seconds"`
		Waiting              string `json:"waiting_seconds"`
	}
	json.Unmarshal(page.Data.Summary, &summary)
	if summary.Runs != 2 || summary.Failed != 1 || summary.Rework != 1 || summary.Execution != "240.000000" || summary.Waiting != "120.000000" {
		t.Fatal("quality/time multiplied by model requests", string(page.Data.Summary))
	}
	q := request
	q.Mode = "compare_quality"
	quality := query("admin", q)
	if len(quality.Data.Items) != 2 || !strings.Contains(string(quality.Data.Items[0])+string(quality.Data.Items[1]), "criteria-v2") {
		t.Fatal("inspection basis or source distinction missing", quality.Data.Items)
	}
	savedComparison := Comparison{Name: "Owned two groups", Left: request, Right: request}
	savedComparison.Left.Basis = basis
	savedComparison.Right.Model = "actual-beta"
	saved := put("comparison:owned", "0", savedComparison)
	if r, err := s.Read(ctx, p, w, saved); err != nil || !strings.Contains(string(r.Value), "context-v1") {
		t.Fatal("saved conditions lost fixed basis", err)
	}
	q = request
	q.Basis = basis
	selected := query("admin", q)
	if len(selected.Data.Items) != 2 {
		t.Fatal("fixed Context/definition selector mismatch", selected.Data.Items)
	}
	price := tariff()
	priceRef := put("price:comparison-public", "0", price)
	eval := Evaluation{Group: observations[0].Group, Kind: "api_equivalent", Usage: []Ref{usageRefs[0]}, Price: &priceRef}
	oldEvaluation := put("evaluation:comparison-old", "0", eval)
	q = request
	q.Kind = "api_equivalent"
	valued := query("admin", q)
	found := false
	for _, b := range valued.Data.Items {
		if strings.Contains(string(mustJSON(b)), `"amount":"0.100000"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("rated amount missing", valued.Data.Items)
	}
	// The other five coverages still appear, including missing and not applicable.
	total := float64(0)
	for _, b := range valued.Data.Items {
		var row map[string]any
		json.Unmarshal(b, &row)
		total += row["records"].(float64)
	}
	if total != 6 {
		t.Fatal("unrated observations disappeared", total)
	}
	price.Rates["input"] = Rate{Amount: "0.002", Per: "1", Unit: "token"}
	newPrice := put(priceRef.ID, priceRef.Version, price)
	eval.Previous = &oldEvaluation
	eval.Price = &newPrice
	put("evaluation:comparison-new", "0", eval)
	detail := q
	detail.Mode = "compare_details"
	details := query("admin", detail)
	found = false
	for _, b := range details.Data.Items {
		if strings.Contains(string(mustJSON(b)), `"amount":"0.200000"`) && strings.Contains(string(b), newPrice.Version) {
			found = true
		}
	}
	if !found {
		t.Fatal("reevaluation or fixed tariff provenance missing", details.Data.Items)
	}
	if _, err := s.Read(ctx, p, w, oldEvaluation); err != nil {
		t.Fatal("old evaluation was overwritten", err)
	}
	// Separate currency and valuation are alternatives, never added together.
	reported := Evaluation{Group: observations[0].Group, Kind: "reported", Usage: []Ref{usageRefs[0]}, Amount: ptr("20"), Currency: "JPY", Source: "owned-report", Scale: 0, Rounding: "half_up"}
	put("evaluation:comparison-yen", "0", reported)
	q.Kind = "reported"
	yen := query("admin", q)
	if !strings.Contains(string(mustJSON(yen.Data.Items)), "JPY") {
		t.Fatal("reported currency missing")
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id=$1 AND id=$2`, w, priceRef.ID)
	q.Kind = "api_equivalent"
	hidden := query("viewer", q)
	if strings.Contains(string(mustJSON(hidden)), "0.200000") || strings.Contains(string(mustJSON(hidden)), "comparison-public") {
		t.Fatal("private tariff leaked through comparison")
	}
	q.Mode = "compare_details"
	hidden = query("viewer", q)
	if strings.Contains(string(mustJSON(hidden)), "comparison-public") {
		t.Fatal("private price leaked through details")
	}
	exec(`UPDATE access_resources SET restricted=true WHERE workspace_id=$1 AND id=$2`, w, contextRef.ID)
	if _, err := s.Read(ctx, access.Principal{UserID: "viewer"}, w, saved); err == nil {
		t.Fatal("comparison condition leaked a private Context")
	}
	q = request
	q.Limit = 1
	first := query("admin", q)
	if first.Meta.NextCursor == nil {
		t.Fatal("comparison did not page")
	}
	q.Cursor = *first.Meta.NextCursor
	next := query("admin", q)
	if next.Data.Through != first.Data.Through || next.Data.AsOf != first.Data.AsOf {
		t.Fatal("comparison page snapshot changed")
	}

	unknown := observation("comparison-uncorrelated", "", "owned-unknown", "777")
	unknown.Coverage = "unknown"
	unknown.Group = "comparison-uncorrelated"
	unknown.At = request.From.Add(time.Hour)
	unknown.Attribution = Attribution{Kind: "run", ID: "run:compare-a", Resource: "run:compare-a", Scopes: []string{"left"}}
	put(ObservationID(unknown), "0", unknown)
	uncertain := query("admin", request)
	found = false
	for _, b := range uncertain.Data.Items {
		var r struct {
			State      string
			Quantities []struct {
				Value     *string
				Uncertain int
			}
		}
		json.Unmarshal(b, &r)
		if r.State == "review_needed" {
			found = true
			for _, q := range r.Quantities {
				if q.Value != nil || q.Uncertain < 1 {
					t.Fatal("uncorrelated consumption was summed as confirmed", string(b))
				}
			}
		}
	}
	if !found {
		t.Fatal("uncorrelated measurement state disappeared")
	}
	t.Log("two saved groups, six coverage records/five attributions, two Scopes, fixed Context/definition, historical inspection/self-report, rework/time, missing/rated/reevaluated/private tariffs and paging: PASS")
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
