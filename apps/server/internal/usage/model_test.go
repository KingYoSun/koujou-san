package usage

import (
	"encoding/json"
	"testing"
	"time"
)

func ptr(s string) *string { return &s }
func quantities() map[string]Quantity {
	return map[string]Quantity{
		"input":     {Value: ptr("100"), State: "reported", Unit: "token", Includes: []string{"cache"}},
		"cache":     {Value: ptr("20"), State: "reported", Unit: "token"},
		"output":    {Value: ptr("50"), State: "reported", Unit: "token", Includes: []string{"reasoning"}},
		"reasoning": {Value: ptr("10"), State: "reported", Unit: "token"},
	}
}
func tariff() Price {
	return Price{Connection: "local-account", Contract: "individual", Model: "actual", Currency: "USD", From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: "fictional fixture", Scale: 6, Rounding: "half_up", Rates: map[string]Rate{
		"input": {Amount: "0.001", Per: "1", Unit: "token"}, "cache": {Amount: "0.0001", Per: "1", Unit: "token"}, "output": {Amount: "0.002", Per: "1", Unit: "token"},
	}}
}
func TestDecimalConsumption(t *testing.T) {
	p := tariff()
	q := quantities()
	check := func(want string) {
		t.Helper()
		v, state, err := calculate(q, p)
		if err != nil || state != "rated" || v == nil || *v != want {
			t.Fatalf("got %v %s %v, want %s", v, state, err, want)
		}
	}
	check("0.182000")
	in := q["input"]
	in.Value = ptr("80")
	in.Includes = nil
	q["input"] = in
	check("0.182000")
	q["image"] = Quantity{Value: ptr("2"), State: "estimated", Unit: "image"}
	p.Rates["image"] = Rate{Amount: "0.03", Per: "1", Unit: "image"}
	check("0.242000")
	q = map[string]Quantity{"input": {Value: ptr("12000000000000001"), State: "reported", Unit: "token"}}
	p.Rates["input"] = Rate{Amount: "0.000000001", Per: "1", Unit: "token"}
	p.Scale = 9
	check("12000000.000000001")
	q["input"] = Quantity{State: "missing", Unit: "token"}
	v, state, err := calculate(q, p)
	if err != nil || v != nil || state != "unrated" {
		t.Fatal(v, state, err)
	}
	q["input"] = Quantity{State: "reported", Value: ptr("0"), Unit: "token"}
	check("0.000000000")
	q["input"] = Quantity{State: "not_applicable", Unit: "token"}
	check("0.000000000")
	for _, c := range []struct{ value, rule, want string }{{"0.0000005", "half_up", "0.000001"}, {"0.0000005", "half_even", "0.000000"}, {"0.0000015", "half_even", "0.000002"}, {"0.0000009", "down", "0.000000"}} {
		n, _ := number(c.value)
		got, e := round(n, 6, c.rule)
		if e != nil || got != c.want {
			t.Fatal(c, got, e)
		}
	}
}
func observation(event, request, source, value string) Observation {
	return Observation{Source: source, Transform: "1", Event: event, Series: event, Mode: "delta", Group: "envelope", Request: request, Coverage: "confirmed", Connection: "local-account", Contract: "individual", ActualModel: ptr("actual"), At: tariff().From, Attribution: Attribution{Kind: "run", ID: "run-1", Scopes: []string{"a", "b"}}, Quantities: map[string]Quantity{"input": {Value: ptr(value), State: "reported", Unit: "token"}}}
}
func record(id string, o Observation) Record {
	raw, _ := json.Marshal(o)
	return Record{Ref: Ref{ID: id, Version: "v1"}, Kind: "usage", Value: raw}
}
func TestCoverageAdoption(t *testing.T) {
	parent := observation("p", "parent", "cli", "200")
	parent.Covers = []string{"child-a", "child-b"}
	rows := []Record{record("p", parent), record("a", observation("a", "child-a", "cli", "70")), record("b", observation("b", "child-b", "cli", "60")), record("g", observation("g", "parent", "gateway", "200"))}
	got, err := combine(rows)
	if err != nil || got.State != "confirmed" || *got.Quantities["input"].Value != "200" || len(got.Refs) != 4 {
		t.Fatal(got, err)
	}
	rows[3] = record("g", observation("g", "parent", "gateway", "201"))
	got, err = combine(rows)
	if err != nil || got.State != "review_needed" {
		t.Fatal(got, err)
	}
	unknown := observation("u", "", "cli", "5")
	unknown.Coverage = "unknown"
	got, err = combine([]Record{record("u", unknown)})
	if err != nil || got.State != "review_needed" {
		t.Fatal(got, err)
	}
}
