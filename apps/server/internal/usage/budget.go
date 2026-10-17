package usage

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"math/big"
	"sort"
	"strings"
)

type CostBudget struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Valuation string `json:"valuation"`
}
type CostState struct {
	State     string              `json:"state"`
	Amount    *string             `json:"amount"`
	Currency  string              `json:"currency"`
	Valuation string              `json:"valuation"`
	Sources   []access.VersionRef `json:"sources"`
	Guarantee string              `json:"guarantee"`
}

func ValidCostBudget(b CostBudget) bool {
	if _, err := number(b.Amount); err != nil {
		return false
	}
	return b.Currency != "" && (b.Valuation == "reported" || b.Valuation == "estimated" || b.Valuation == "api_equivalent" || b.Valuation == "allocated")
}
func uniqueBudgetRefs(refs []access.VersionRef) []access.VersionRef {
	out := []access.VersionRef{}
	seen := map[access.VersionRef]bool{}
	for _, ref := range refs {
		if !seen[ref] {
			out = append(out, ref)
			seen[ref] = true
		}
	}
	return out
}
func ObserveRunCosts(ctx context.Context, q query, p access.Principal, w string, ids []string, b CostBudget) (CostState, error) {
	out := CostState{State: "observed", Currency: b.Currency, Valuation: b.Valuation, Sources: []access.VersionRef{}, Guarantee: "further_starts_only"}
	required := map[string]bool{}
	for _, id := range ids {
		required[id] = false
	}
	var err error
	total := new(big.Rat)
	scale := 0
	for run := range required {
		entries, err := q.Query(ctx, `SELECT DISTINCT ON(e.projection_key) e.attribution_id,e.amount::text,e.currency,e.state,e.refs,EXISTS(SELECT 1 FROM usage_group_updates u WHERE u.workspace_id=e.workspace_id AND u.coverage_group=e.coverage_group AND u.position>e.position) FROM usage_entries e WHERE e.workspace_id=$1 AND e.attribution_kind='run' AND e.attribution_id=$2 AND e.kind=$3 ORDER BY e.projection_key,e.position DESC`, w, run, b.Valuation)
		if err != nil {
			return out, err
		}
		for entries.Next() {
			var run, currency, state string
			var amount *string
			var refs []byte
			var stale bool
			if err = entries.Scan(&run, &amount, &currency, &state, &refs, &stale); err != nil {
				break
			}
			var deps []access.VersionRef
			if err = json.Unmarshal(refs, &deps); err != nil {
				break
			}
			out.Sources = append(out.Sources, deps...)
			if amount == nil || currency != b.Currency || state != "rated" || stale {
				out.State = "unobserved"
				continue
			}
			n, ok := new(big.Rat).SetString(*amount)
			if !ok {
				out.State = "unobserved"
				continue
			}
			total.Add(total, n)
			_, fraction, _ := strings.Cut(*amount, ".")
			if len(fraction) > scale {
				scale = len(fraction)
			}
			required[run] = true
		}
		if err == nil {
			err = entries.Err()
		}
		entries.Close()
		if err != nil {
			return out, err
		}
	}
	for _, found := range required {
		if !found {
			out.State = "unobserved"
		}
	}
	out.Sources = uniqueBudgetRefs(out.Sources)
	sort.Slice(out.Sources, func(i, j int) bool {
		if out.Sources[i].ID == out.Sources[j].ID {
			return out.Sources[i].Version < out.Sources[j].Version
		}
		return out.Sources[i].ID < out.Sources[j].ID
	})
	var allowed bool
	refsJSON, _ := json.Marshal(out.Sources)
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM access_members WHERE workspace_id=$1 AND user_id=$2 AND active) AND `+access.ReadFilterSQL("$4::jsonb"), w, p.UserID, p.OperationID, refsJSON).Scan(&allowed); err != nil {
		return out, err
	}
	if !allowed {
		return out, access.ErrForbidden
	}
	if out.State == "observed" {
		text := total.FloatString(scale)
		out.Amount = &text
		limit, ok := new(big.Rat).SetString(b.Amount)
		if !ok {
			return out, ErrInvalid
		}
		if total.Cmp(limit) >= 0 {
			out.State = "exhausted"
		}
	}
	return out, nil
}
