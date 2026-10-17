package hopper

import (
	"encoding/json"
	"testing"
)

func TestDeterministicConditions(t *testing.T) {
	leaf := func(field, value string) Condition {
		return Condition{Field: field, Op: "eq", Value: json.RawMessage(`"` + value + `"`)}
	}
	failure := leaf("ci.conclusion", "failure")
	for _, tc := range []struct {
		conclusion string
		want       string
	}{{"failure", "true"}, {"cancelled", "false"}, {"timed_out", "false"}, {"success", "false"}, {"new-provider-value", "unknown"}, {"", "unknown"}} {
		if got := Evaluate(failure, Facts{Conclusion: tc.conclusion}); got != tc.want {
			t.Fatalf("%s: %s", tc.conclusion, got)
		}
	}
	f := Facts{Conclusion: "unrecognized", State: "failed"}
	for _, tc := range []struct {
		c    Condition
		want string
	}{
		{Condition{All: []Condition{failure, leaf("run.state", "succeeded")}}, "false"},
		{Condition{Any: []Condition{failure, leaf("run.state", "failed")}}, "true"},
		{Condition{All: []Condition{failure, leaf("run.state", "failed")}}, "unknown"},
		{Condition{Not: &failure}, "unknown"},
	} {
		if got := Evaluate(tc.c, f); got != tc.want {
			t.Fatalf("%+v: %s", tc.c, got)
		}
	}
	zero := 0
	if got := Evaluate(failure, Facts{ExitCode: &zero}); got != "unknown" {
		t.Fatal("observer exit zero fabricated CI success", got)
	}
	if validCondition(Condition{Field: "ci.conclusion", Op: "shell", Value: json.RawMessage(`"echo unsafe"`)}) {
		t.Fatal("accepted executable condition")
	}
	if got := Evaluate(leaf("result.problem", "bug"), Facts{Report: json.RawMessage(`{"problem":"bug"}`)}); got != "true" {
		t.Fatal(got)
	}
}
