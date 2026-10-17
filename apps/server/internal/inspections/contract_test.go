package inspections

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func fixtureDefinition() Definition {
	return Definition{Name: "fixed inspection", Kind: "InspectionRun", Purpose: "contract", Template: Ref{ID: "run-definition:inspector", Version: "one"}, Targets: []contexts.Fixed{{URI: "fixture://artifact", Revision: "commit-one", Digest: strings.Repeat("a", 64), Range: "tree", Delivery: "reference", State: "ready"}}, Criteria: []Criterion{{ID: "AC-1", Requirement: "the selected behavior holds"}}, Selection: contexts.Selection{Mode: "none"}, Required: map[string]string{}}
}
func TestStructuredReportKeepsRawOutputAndDoesNotTurnMissingEvidenceIntoPass(t *testing.T) {
	d := fixtureDefinition()
	now := time.Now().UTC()
	e := Ref{ID: "inspection-evidence:one", Version: "fixed"}
	l := Launch{Definition: Ref{ID: "inspection:one", Version: "one"}, Manifest: contexts.Manifest{Digest: "fixed-input"}}
	report := Report{Inspection: l.Definition, ManifestDigest: l.Manifest.Digest, Verdict: "PASS", Unverified: []string{}, Results: []CriterionResult{{ID: "AC-1", Verdict: "PASS", Evidence: []Ref{e}, Executed: true, Unverified: []string{}}}}
	availableEvidence := map[Ref]Evidence{e: {State: "available", Kind: "machine-observation", CheckedAt: now, Description: "fixed proof", Targets: d.Targets, Criteria: []string{"AC-1"}}}
	base := &runs.Assessment{State: "valid", Verdict: "PASS"}
	tests := []struct {
		name, want string
		change     func(*Report)
	}{
		{"valid", "PASS", func(r *Report) {}},
		{"exit-zero FAIL", "FAIL", func(r *Report) { r.Verdict = "FAIL"; r.Results[0].Verdict = "FAIL" }},
		{"unexecuted", "INCONCLUSIVE", func(r *Report) { r.Results[0].Executed = false }},
		{"missing evidence", "INCONCLUSIVE", func(r *Report) { r.Results[0].Evidence = nil }},
		{"unverified", "INCONCLUSIVE", func(r *Report) { r.Unverified = []string{"not checked"} }},
		{"missing criterion", "INCONCLUSIVE", func(r *Report) { r.Results = nil }},
		{"outside declared criteria", "INCONCLUSIVE", func(r *Report) { r.Results[0].ID = "UNREQUESTED" }},
		{"different input", "INCONCLUSIVE", func(r *Report) { r.ManifestDigest = "other" }},
		{"missing unverified", "INCONCLUSIVE", func(r *Report) { r.Unverified = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input Report
			_ = json.Unmarshal(raw(report), &input)
			tt.change(&input)
			original := raw(input)
			got := assess(d, l, original, base, availableEvidence, now)
			if got.Verdict != tt.want || string(got.Report) != string(original) {
				t.Fatal(got)
			}
		})
	}
	invalid := assess(d, l, json.RawMessage(`"natural language PASS"`), base, availableEvidence, now)
	if invalid.State != "invalid" || invalid.Verdict == "PASS" {
		t.Fatal(invalid)
	}
	l.Spec.Derivation = &runs.Contract{VerdictField: "decision"}
	var renamed map[string]json.RawMessage
	_ = json.Unmarshal(raw(report), &renamed)
	renamed["decision"] = renamed["verdict"]
	delete(renamed, "verdict")
	originalRenamed := raw(renamed)
	if projected := assess(d, l, originalRenamed, base, availableEvidence, now); projected.Verdict != "PASS" || string(projected.Report) != string(originalRenamed) {
		t.Fatal("explicit verdict projection changed original", projected)
	}
	base.Verdict = "FAIL"
	if assess(d, l, originalRenamed, base, availableEvidence, now).Verdict != "FAIL" {
		t.Fatal("base FAIL became PASS")
	}
	base.Verdict = "PASS"
	l.Spec.Derivation = nil
	wrong := availableEvidence[e]
	wrong.Criteria = []string{"OTHER"}
	availableEvidence[e] = wrong
	if assess(d, l, raw(report), base, availableEvidence, now).Verdict == "PASS" {
		t.Fatal("evidence for other criterion passed")
	}
	wrong.Criteria = []string{"AC-1"}
	wrong.Targets = []contexts.Fixed{}
	availableEvidence[e] = wrong
	if assess(d, l, raw(report), base, availableEvidence, now).Verdict == "PASS" {
		t.Fatal("evidence without fixed target passed")
	}
	expires := now.Add(-time.Second)
	availableEvidence[e] = Evidence{State: "available", ExpiresAt: &expires}
	expired := assess(d, l, raw(report), base, availableEvidence, now)
	if expired.Verdict == "PASS" {
		t.Fatal("expired evidence accepted")
	}
	d.Reuse = &Reuse{Run: Ref{ID: "run:previous", Version: "fixed"}, Criteria: []string{"AC-1"}, Changes: []string{"changed one function"}}
	availableEvidence[e] = Evidence{State: "available"}
	if assess(d, l, raw(report), base, availableEvidence, now).Verdict == "PASS" {
		t.Fatal("unsupported differential reuse accepted")
	}
}
func TestDeclarationsDoNotClaimEnvironmentEnforcement(t *testing.T) {
	d := fixtureDefinition()
	if validate(d) != nil {
		t.Fatal("valid definition rejected")
	}
	claims := separation([]string{"inspection/session", "inspection/permissions/enforced"})
	if claims["session"].Level != "declared" || claims["permissions"].Level != "unobserved" || len(gaps(map[string]string{"session": "enforced"}, claims)) != 1 {
		t.Fatal(claims)
	}
	if len(gaps(map[string]string{"criteria": "enforced", "context": "enforced"}, claims)) != 0 {
		t.Fatal("fixed-input guarantee missing")
	}
	d.Purpose = "fresh"
	d.Selection.Mode = "select"
	if validate(d) == nil {
		t.Fatal("fresh review implicitly injected Context")
	}
}
