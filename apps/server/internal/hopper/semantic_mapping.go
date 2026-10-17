package hopper

import (
	"fmt"
	"strings"
)

// The source result is immutable. Mappings can explicitly select semantic
// findings; the evaluator's full input and conversation are never mapped.
func evaluationData(e *Evaluation) any {
	value := decodeValue(e.Source.Facts.Report)
	if e.Semantic == nil || e.Semantic.Decision == nil {
		return value
	}
	out, ok := value.(map[string]any)
	if !ok {
		out = map[string]any{}
	}
	decision := *e.Semantic.Decision
	if decision.Candidate != nil {
		finding := *decision.Candidate
		label := "疑い（未検証）"
		if finding.Status == "needs_reproduction" {
			label = "要再現"
		}
		finding.Title = "[" + label + "] " + finding.Title
		var proof strings.Builder
		fmt.Fprintf(&proof, "状態: %s\n\n%s\n\n根拠:\n", label, finding.Body)
		for _, citation := range decision.Evidence {
			fmt.Fprintf(&proof, "- %s @ %s: %s\n", citation.Ref.ID, citation.Ref.Version, citation.Quote)
		}
		finding.Body = proof.String()
		decision.Candidate = &finding
	}
	out["semantic"] = decodeValue(raw(decision))
	return out
}
