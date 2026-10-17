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
func ObserveCosts(ctx context.Context, q query, p access.Principal, w, kind string, ids []string, b CostBudget) (CostState, error) {
	out := CostState{State: "observed", Currency: b.Currency, Valuation: b.Valuation, Sources: []access.VersionRef{}, Guarantee: "further_starts_only"}
	required := map[string]bool{}
	for _, id := range ids {
		required[id] = false
	}
	var err error
	total := new(big.Rat)
	scale := 0
	for run := range required {
		entries, err := q.Query(ctx, `WITH groups AS (SELECT DISTINCT coverage_group FROM usage_entries WHERE workspace_id=$1 AND attribution_kind=$4 AND attribution_id=$2 AND kind IN ('usage',$3))
 SELECT $2::text,e.amount::text,COALESCE(e.currency,''),COALESCE(e.state,'unrated'),COALESCE(e.refs,'[]'::jsonb)||COALESCE(raw.refs,'[]'::jsonb),EXISTS(SELECT 1 FROM usage_group_updates u WHERE u.workspace_id=$1 AND u.coverage_group=g.coverage_group AND u.position>e.position)
 FROM groups g LEFT JOIN LATERAL (SELECT refs FROM usage_entries WHERE workspace_id=$1 AND coverage_group=g.coverage_group AND kind='usage' ORDER BY position DESC LIMIT 1) raw ON true
 LEFT JOIN LATERAL (SELECT DISTINCT ON(projection_key) * FROM usage_entries WHERE workspace_id=$1 AND coverage_group=g.coverage_group AND kind=$3 ORDER BY projection_key,position DESC) e ON true`, w, run, b.Valuation, kind)
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
