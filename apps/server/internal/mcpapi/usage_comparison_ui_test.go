package mcpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

func seedUsageComparisonUI(t *testing.T, ctx context.Context, s *Service, p access.Principal, cfg auth.Config) {
	t.Helper()
	const w = "mcp-one"
	serial := 0
	m := func(v string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: fmt.Sprint("cost-ui-", serial), ExpectedVersion: v}
	}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	must := func(v apicontract.Response[apicontract.Reference], err error) usage.Ref {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatal("owned cost fixture", v.Error, err)
		}
		return usage.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	for _, id := range []string{"cost-a", "cost-b"} {
		must(s.Contexts.Put(ctx, p, w, contexts.Identity{Kind: "scope", ID: id}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Scope{Kind: "project", Name: "費用比較 " + id})}))
	}
	text := "入力エラーを検出する規約。最初の入力版。"
	c := must(s.Contexts.Put(ctx, p, w, contexts.Identity{Kind: "sentence", ID: "cost-basis"}, contexts.Write{Mutation: m("0"), Value: raw(contexts.Unit{Kind: "sentence", Name: "費用比較の規約", Text: &text})}))
	basis := []contexts.Ref{{Kind: "sentence", ID: "cost-basis", Revision: c.Version}}
	text = "入力エラーの条件と確認方法を明確にした規約。二つ目の入力版。"
	c = must(s.Contexts.Put(ctx, p, w, contexts.Identity{Kind: "sentence", ID: "cost-basis"}, contexts.Write{Mutation: m(c.Version), Value: raw(contexts.Unit{Kind: "sentence", Name: "費用比較の規約", Text: &text})}))
	basis = append(basis, contexts.Ref{Kind: "sentence", ID: "cost-basis", Revision: c.Version})
	definitions := []usage.Ref{}
	runRefs := []usage.Ref{}
	definitionVersion := "0"
	for i, ref := range basis {
		selection := contexts.Selection{Mode: "select", Include: []contexts.Ref{ref}, Exclude: []contexts.Ref{}}
		spec := runs.Spec{Name: fmt.Sprintf("受入: 費用比較 %c", 'A'+i), Input: runs.Input{Prompt: raw("所有する比較用の合成入力。外部への要求やCLI起動は行わない。"), Command: runs.Command{Kind: "cli", Target: "owned-generator", Parameters: raw([]string{}), Binding: map[string]string{"prompt": "stdin", "context": "stdin"}}}}
		definition := workflows.Definition{Schema: "run-definition-v1", Name: fmt.Sprintf("費用比較の工程 v%d", i+1), Spec: &spec, Selection: &selection, Inputs: []string{}}
		d := must(s.ClientOps.Definitions.Put(ctx, p, w, "run-definition:cost-ui", workflows.Write{Mutation: m(definitionVersion), Value: raw(definition)}))
		definitionVersion = d.Version
		definitions = append(definitions, d)
		id := fmt.Sprintf("cost-ui-%c", 'a'+i)
		manifest := must(s.Contexts.Put(ctx, p, w, contexts.Identity{Kind: "manifest", ID: id}, contexts.Write{Mutation: m("0"), Value: raw(contexts.ResolveRequest{RunID: id, Selection: selection})}))
		spec.Manifest = &contexts.Ref{Kind: "manifest", ID: id, Revision: manifest.Version}
		spec.Sources = []runs.Ref{{ID: d.ID, Version: d.Version}}
		source, _ := generationSourceFixture(t, ctx, s, p, cfg.Key, id, contexts.Ref{}, spec)
		runRefs = append(runRefs, usage.Ref{ID: source.Run.ID, Version: source.Run.Version})
	}
	from := time.Now().UTC().Truncate(24 * time.Hour)
	price := usage.Price{Connection: "owned-cost", Contract: "owned-individual", Model: "owned-cost-alpha", Currency: "USD", From: from.Add(-time.Hour), Source: "所有する合成単価（実契約・実請求ではない）", Scale: 6, Rounding: "half_up", Rates: map[string]usage.Rate{"input": {Amount: "0.001", Per: "1", Unit: "token"}}}
	priceRef := must(s.Usage.Put(ctx, p, w, "price:cost-ui", usage.Write{Mutation: m("0"), Value: raw(price)}))
	for i, kind := range []string{"run", "run", "operation", "hopper", "triage", "unattributed"} {
		model := "owned-cost-alpha"
		value := "100"
		b := 0
		owner := ""
		billing := "api"
		state := "final"
		if i == 1 {
			model = "owned-cost-beta"
			value = "60"
			b = 1
			billing = "subscription"
			state = "partial"
		}
		if kind == "run" {
			owner = runRefs[b].ID
		} else if kind != "unattributed" {
			owner = fmt.Sprint("owned-", kind, "-cost")
		}
		quantity := usage.Quantity{Value: &value, State: "reported", Unit: "token"}
		if i == 2 {
			quantity = usage.Quantity{State: "missing", Unit: "token", Reason: "所有fixtureの未取得"}
		}
		if i == 5 {
			billing = "local"
			state = "not_applicable"
			quantity = usage.Quantity{State: "not_applicable", Unit: "token"}
		}
		o := usage.Observation{Source: "owned-cost-ui", Transform: "1", Event: fmt.Sprint("owned-cost-", i), Series: fmt.Sprint("owned-cost-", i), Mode: "delta", Group: fmt.Sprint("cost-ui-group-", i), Request: fmt.Sprint("owned-cost-request-", i), Coverage: "confirmed", Connection: "owned-cost", Contract: "owned-individual", ActualModel: &model, At: from.Add(time.Hour), Attribution: usage.Attribution{Kind: kind, ID: owner, Resource: runRefs[b].ID, Scopes: []string{"cost-a", "cost-b"}}, Quantities: map[string]usage.Quantity{"input": quantity}, Measurement: &usage.Measurement{State: state, Protocol: "owned-fixture", Billing: billing, Scope: "合成の主セッション（補助処理は未取得）", Generation: "owned"}}
		deps := []usage.Ref{{ID: "context:cost-basis", Version: basis[b].Revision}, definitions[b]}
		u := must(s.Usage.PutFromOwner(ctx, p, w, usage.ObservationID(o), usage.Write{Mutation: m("0"), Value: raw(o)}, usage.OwnerWrite{Actor: "owned-cost-fixture", Dependencies: deps, Check: func(context.Context, pgx.Tx) error { return nil }}))
		if i == 0 {
			must(s.Usage.Put(ctx, p, w, "evaluation:cost-ui", usage.Write{Mutation: m("0"), Value: raw(usage.Evaluation{Group: o.Group, Kind: "api_equivalent", Usage: []usage.Ref{u}, Price: &priceRef})}))
		}
	}
	left := usage.Request{Mode: "compare", Kind: "api_equivalent", From: from, Until: from.Add(24 * time.Hour), Timezone: "Asia/Tokyo", Scopes: []string{"cost-a", "cost-b"}, Limit: 50, Model: "owned-cost-alpha"}
	right := left
	right.Model = "owned-cost-beta"
	comparison := must(s.Usage.Put(ctx, p, w, "comparison:cost-ui", usage.Write{Mutation: m("0"), Value: raw(usage.Comparison{Name: "受入: 同程度の作業を二群で比較", Left: left, Right: right})}))
	t.Logf("Owned comparison %s @ %s; Context fixed versions %s / %s; definition versions %s / %s; no provider requests", comparison.ID, comparison.Version, basis[0].Revision, basis[1].Revision, definitions[0].Version, definitions[1].Version)
}
func usageComparisonUIEvidence(t *testing.T, s *Service, ctx context.Context, site string) {
	t.Helper()
	rows, err := s.Usage.Store.Database().Query(ctx, `SELECT jsonb_build_object('id',id,'version',version,'kind',kind,'value',payload) FROM usage_records WHERE workspace_id='mcp-one' AND (kind='comparison' OR id LIKE 'price:cost-ui%' OR id LIKE 'evaluation:cost-ui%' OR id LIKE 'evaluation:%') ORDER BY position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	records := []json.RawMessage{}
	for rows.Next() {
		var b json.RawMessage
		if err = rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		records = append(records, b)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	b, _ := json.MarshalIndent(map[string]any{"site": site, "records": records}, "", "  ")
	if err = os.WriteFile("../../../../test-results/issue32-usage-ui.json", b, 0600); err != nil {
		t.Fatal(err)
	}
}
