package mcpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Called with the real Ory resource-scoped token and official HTTP SDK fixture.
func checkUsageMCP(t *testing.T, s *Service, p access.Principal, ordinary string, session, editor, none *mcp.ClientSession,
	connect func(string) (*mcp.ClientSession, error), call func(*mcp.ClientSession, string, any, bool) []byte,
	request func(string, string, any, map[string]string) (*http.Response, []byte), site string) {
	t.Helper()
	ctx := context.Background()
	price := usage.Price{Connection: "client-personal", Contract: "individual", Model: "actual-fixture", Currency: "USD", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: "fictional contract", Scale: 6, Rounding: "half_up", Rates: map[string]usage.Rate{"input": {Amount: "0.001", Per: "1", Unit: "token"}}}
	raw, _ := json.Marshal(price)
	write := usage.Write{Mutation: apicontract.Mutation{OperationID: "usage-price-first", ExpectedVersion: "0"}, Value: raw}
	args := priceWrite{ID: "price:personal", Write: write}
	call(session, "price_set", args, true)
	call(editor, "price_set", args, true)
	// The ordinary client explicitly requests the new purpose; Context edit does
	// not acquire price writes, and the model cannot grant its own operation.
	res, body := request("POST", site+"/mcp/control/workspaces/mcp-one/operations/pricing", map[string]any{"purpose": "price-edit", "operation_id": "price-operation", "expected_version": "0"}, map[string]string{"Authorization": "Bearer " + ordinary})
	var created struct {
		Handle string `json:"handle"`
	}
	_ = json.Unmarshal(body, &created)
	if res.StatusCode != 200 || created.Handle == "" {
		t.Fatal("price control purpose", res.StatusCode, string(body))
	}
	pricing, err := connect(created.Handle)
	if err != nil {
		t.Fatal(err)
	}
	defer pricing.Close()
	first := call(pricing, "price_set", args, false)
	var saved apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(first, &saved)
	if saved.Data == nil {
		t.Fatal(string(first))
	}
	res, body = request("POST", site+"/usage/workspaces/mcp-one/records/price:personal", write, map[string]string{"Authorization": "Bearer " + ordinary})
	var replay apicontract.Response[apicontract.Reference]
	_ = json.Unmarshal(body, &replay)
	if res.StatusCode != 200 || replay.Data == nil || *saved.Data != *replay.Data {
		t.Fatal("MCP/HTTP price replay differs", string(body))
	}
	ref := usage.Ref{ID: saved.Data.ID, Version: saved.Data.Version}
	mcpBody := call(session, "usage_get", ref, false)
	res, httpBody := request("GET", site+"/usage/workspaces/mcp-one/records/price:personal?version="+ref.Version, nil, map[string]string{"Authorization": "Bearer " + ordinary})
	var m, h usage.Record
	_ = json.Unmarshal(mcpBody, &m)
	_ = json.Unmarshal(httpBody, &h)
	if res.StatusCode != 200 || m.Ref != h.Ref || string(m.Value) != string(h.Value) {
		t.Fatal("fixed tariff differs across boundaries")
	}
	call(none, "usage_get", ref, true)
	actual, quantity := "actual-fixture", "100"
	observation := usage.Observation{Source: "fixture-cli", Transform: "1", Event: "usage-example", Series: "usage-example", Mode: "delta", Group: "usage-example", Request: "usage-example", Coverage: "confirmed", Connection: price.Connection, Contract: price.Contract, ActualModel: &actual, At: time.Now().UTC(), Attribution: usage.Attribution{Kind: "unattributed", Scopes: []string{}}, Quantities: map[string]usage.Quantity{"input": {Value: &quantity, State: "reported", Unit: "token"}}}
	value, _ := json.Marshal(observation)
	observed, e := s.Usage.Put(ctx, p, "mcp-one", usage.ObservationID(observation), usage.Write{Mutation: apicontract.Mutation{OperationID: "usage-example", ExpectedVersion: "0"}, Value: value})
	if e != nil || observed.Error != nil {
		t.Fatal("usage fixture", observed, e)
	}
	value, _ = json.Marshal(usage.Evaluation{Group: observation.Group, Kind: "estimated", Price: &ref})
	evaluated, e := s.Usage.Put(ctx, p, "mcp-one", "evaluation:usage-example", usage.Write{Mutation: apicontract.Mutation{OperationID: "usage-example-evaluate", ExpectedVersion: "0"}, Value: value})
	if e != nil || evaluated.Error != nil {
		t.Fatal("cost fixture", evaluated, e)
	}
	for _, query := range []usage.Request{
		{Mode: "compare", Kind: "estimated", From: price.From, Until: price.From.AddDate(1, 0, 0), Limit: 50},
		{Mode: "compare_details", Kind: "estimated", From: price.From, Until: price.From.AddDate(1, 0, 0), Limit: 50},
		{Mode: "records", Kind: "usage", Limit: 50}, {Mode: "records", Kind: "evaluation", Limit: 50},
		{Mode: "aggregate", Kind: "estimated", From: price.From, Until: price.From.AddDate(1, 0, 0), Limit: 50},
		{Mode: "export", Kind: "estimated", From: price.From, Until: price.From.AddDate(1, 0, 0), Limit: 50},
	} {
		mcpRaw := call(session, "usage_query", query, false)
		res, httpRaw := request("POST", site+"/usage/workspaces/mcp-one/query", query, map[string]string{"Authorization": "Bearer " + ordinary})
		var mcpPage, httpPage apicontract.Response[usage.Page]
		_ = json.Unmarshal(mcpRaw, &mcpPage)
		_ = json.Unmarshal(httpRaw, &httpPage)
		if res.StatusCode != 200 || mcpPage.Data == nil || httpPage.Data == nil || len(mcpPage.Data.Items) != 1 || string(mcpPage.Data.Items[0]) != string(httpPage.Data.Items[0]) {
			t.Fatal("usage/cost page differs across boundaries", query.Mode, query.Kind, string(mcpRaw), string(httpRaw))
		}
	}
	setPricePolicy := func(restricted bool) {
		t.Helper()
		if err := pgx.BeginFunc(ctx, s.Access.DB, func(tx pgx.Tx) error {
			return s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: "mcp-one", ID: ref.ID, Restricted: restricted}, nil)
		}); err != nil {
			t.Fatal(err)
		}
	}
	setPricePolicy(true)
	call(session, "usage_get", ref, true)
	call(session, "usage_get", usage.Ref{ID: evaluated.Data.ID, Version: evaluated.Data.Version}, true)
	for _, mode := range []string{"aggregate", "export", "compare", "compare_details"} {
		body := call(session, "usage_query", usage.Request{Mode: mode, Kind: "estimated", From: price.From, Until: price.From.AddDate(1, 0, 0), Limit: 50}, false)
		var page apicontract.Response[usage.Page]
		_ = json.Unmarshal(body, &page)
		if page.Data == nil {
			t.Fatal("private query unavailable", mode, string(body))
		}
		if mode == "compare" || mode == "compare_details" {
			if strings.Contains(string(body), "price:personal") || strings.Contains(string(body), `"amount":"0.100000"`) {
				t.Fatal("private price leaked through comparison", mode, string(body))
			}
		} else if len(page.Data.Items) != 0 {
			t.Fatal("private price basis leaked via MCP", mode, string(body))
		}
	}
	setPricePolicy(false)
	other, e := s.Usage.Put(ctx, p, "mcp-two", "price:other", usage.Write{Mutation: apicontract.Mutation{OperationID: "other-price", ExpectedVersion: "0"}, Value: raw})
	if e != nil || other.Data == nil {
		t.Fatal(other, e)
	}
	call(session, "usage_get", usage.Ref{ID: other.Data.ID, Version: other.Data.Version}, true)
	q := usage.Request{Mode: "records", Kind: "price", Limit: 1}
	body = call(session, "usage_query", q, false)
	var page apicontract.Response[usage.Page]
	_ = json.Unmarshal(body, &page)
	if page.Data == nil || len(page.Data.Items) != 1 {
		t.Fatal("MCP price page", string(body))
	}
	body = call(none, "usage_query", q, false)
	_ = json.Unmarshal(body, &page)
	if page.Data == nil || len(page.Data.Items) != 0 {
		t.Fatal("none cost query broadened")
	}
	// Revocation is evaluated again on an otherwise valid idempotent replay.
	if _, err = s.Access.DB.Exec(ctx, `UPDATE access_members SET role='viewer' WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); err != nil {
		t.Fatal(err)
	}
	call(pricing, "price_set", args, true)
	if _, err = s.Access.DB.Exec(ctx, `UPDATE access_members SET role='admin' WHERE workspace_id='mcp-one' AND user_id=$1`, p.UserID); err != nil {
		t.Fatal(err)
	}
	t.Log("real Ory + MCP/HTTP manual individual tariff: fixed versions, replay, explicit price purpose, none boundary and current role passed")
}
