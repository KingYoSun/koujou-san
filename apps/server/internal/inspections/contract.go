package inspections

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func decode(value []byte, target any) error {
	if !json.Valid(value) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(value))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return ErrInvalid
	}
	return nil
}
func validate(d Definition) error {
	if d.Name == "" || (d.Kind != "InspectionRun" && d.Kind != "HumanReviewRun") || (d.Purpose != "contract" && d.Purpose != "fresh" && d.Purpose != "human") || !fixed(d.Template) || len(d.Targets) == 0 || len(d.Criteria) == 0 || d.Required == nil {
		return ErrInvalid
	}
	if d.Selection.Mode != "none" && d.Selection.Mode != "select" {
		return ErrInvalid
	}
	if d.Purpose == "fresh" && d.Selection.Mode != "none" {
		return ErrInvalid
	}
	ids := map[string]bool{}
	for _, c := range d.Criteria {
		if strings.TrimSpace(c.ID) == "" || ids[c.ID] || strings.TrimSpace(c.Requirement) == "" {
			return ErrInvalid
		}
		ids[c.ID] = true
	}
	for _, target := range d.Targets {
		if contexts.ValidateFixed(target) != nil {
			return ErrInvalid
		}
	}
	for dimension, level := range d.Required {
		if !slices.Contains(dimensions, dimension) || rank(level) == 0 {
			return ErrInvalid
		}
	}
	for dimension, refs := range d.Observations {
		if !slices.Contains(dimensions, dimension) || len(refs) == 0 {
			return ErrInvalid
		}
		for _, ref := range refs {
			if !fixed(ref) {
				return ErrInvalid
			}
		}
	}
	if d.Reuse != nil {
		r := d.Reuse
		if !fixed(r.Run) || len(r.Criteria) == 0 || len(r.Changes) == 0 {
			return ErrInvalid
		}
		for _, id := range r.Criteria {
			if !ids[id] {
				return ErrInvalid
			}
		}
	}
	return nil
}

// A device's capability declaration remains a declaration even if its text says
// "enforced". Platform guarantees are constructed by the owning input path.
func separation(capabilities []string) map[string]Separation {
	out := map[string]Separation{}
	for _, dimension := range dimensions {
		out[dimension] = Separation{Level: "unobserved", Origin: "declaration"}
		if slices.Contains(capabilities, "inspection/"+dimension) {
			out[dimension] = Separation{Level: "declared", Origin: "declaration", Method: "connection-capability"}
		}
	}
	out["criteria"] = Separation{Level: "enforced", Origin: "platform", Method: "fixed-version-basis"}
	out["context"] = Separation{Level: "enforced", Origin: "platform", Method: "fixed-manifest-injection"}
	return out
}
func gaps(required map[string]string, actual map[string]Separation) []string {
	out := []string{}
	for _, dimension := range dimensions {
		level, ok := required[dimension]
		if ok && rank(actual[dimension].Level) < rank(level) {
			out = append(out, dimension)
		}
	}
	return out
}
func available(e Evidence, at time.Time) bool {
	return e.State == "available" && (e.ExpiresAt == nil || at.Before(*e.ExpiresAt))
}
func targetKey(t contexts.Fixed) string {
	return string(raw([]string{t.URI, t.Revision, t.Range, t.Digest}))
}
func supports(e Evidence, id string) bool {
	return strings.TrimSpace(e.Description) != "" && slices.Contains(e.Criteria, id)
}

// An explicitly selected top-level verdict field is a deterministic projection.
// The stored Report is never replaced with this projection.
func reportValue(launch Launch, value json.RawMessage) (Report, error) {
	field := "verdict"
	if launch.Spec.Derivation != nil && launch.Spec.Derivation.VerdictField != "" {
		field = launch.Spec.Derivation.VerdictField
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(value, &object) != nil {
		return Report{}, ErrInvalid
	}
	if field != "verdict" {
		if _, duplicate := object["verdict"]; duplicate {
			return Report{}, ErrInvalid
		}
		object["verdict"] = object[field]
		delete(object, field)
	}
	var r Report
	err := decode(raw(object), &r)
	return r, err
}

// The original Report bytes stay in Run.output/result. This derives only the
// accepted verdict for the fixed basis, not the truth of external assertions.
func assess(d Definition, launch Launch, report json.RawMessage, base *runs.Assessment, evidence map[Ref]Evidence, at time.Time) *runs.Assessment {
	result := &runs.Assessment{State: "valid", Verdict: "INCONCLUSIVE", Report: report}
	r, err := reportValue(launch, report)
	if base == nil || base.State != "valid" || err != nil || r.Unverified == nil || r.Inspection != launch.Definition || r.ManifestDigest != launch.Manifest.Digest || !verdict(r.Verdict) {
		result.State = "invalid"
		return result
	}
	expected := map[string]bool{}
	for _, c := range d.Criteria {
		expected[c.ID] = true
	}
	seen := map[string]bool{}
	accepted := "PASS"
	for _, c := range r.Results {
		if !expected[c.ID] || seen[c.ID] || c.Unverified == nil || !verdict(c.Verdict) {
			result.State = "invalid"
			return result
		}
		seen[c.ID] = true
		if c.Verdict == "FAIL" {
			accepted = "FAIL"
		}
		incomplete := !c.Executed || len(c.Unverified) > 0 || len(c.Evidence) == 0 || c.Verdict == "INCONCLUSIVE"
		covered := map[string]bool{}
		for _, ref := range c.Evidence {
			e, ok := evidence[ref]
			if !ok || !available(e, at) || !supports(e, c.ID) {
				incomplete = true
			} else {
				for _, target := range e.Targets {
					covered[targetKey(target)] = true
				}
			}
		}
		for _, target := range d.Targets {
			if !covered[targetKey(target)] {
				incomplete = true
			}
		}
		if incomplete && accepted != "FAIL" {
			accepted = "INCONCLUSIVE"
		}
	}
	if len(seen) != len(expected) {
		result.State = "invalid"
		return result
	}
	if len(r.Unverified) > 0 && accepted == "PASS" {
		accepted = "INCONCLUSIVE"
	}
	if r.Verdict == "FAIL" || base.Verdict == "FAIL" {
		accepted = "FAIL"
	} else if (r.Verdict == "INCONCLUSIVE" || base.Verdict == "INCONCLUSIVE") && accepted == "PASS" {
		accepted = "INCONCLUSIVE"
	}
	if d.Reuse != nil && (len(d.Reuse.Impact) == 0 || len(d.Reuse.Evidence) == 0 || len(d.Reuse.Unverified) > 0) && accepted == "PASS" {
		accepted = "INCONCLUSIVE"
	}
	if d.Reuse != nil && accepted == "PASS" {
		for _, ref := range d.Reuse.Evidence {
			if e, ok := evidence[ref]; !ok || !available(e, at) {
				accepted = "INCONCLUSIVE"
			}
		}
	}
	result.Verdict = accepted
	return result
}
