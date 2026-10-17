package workflows

import (
	"context"
	"encoding/json"
	"math/big"
	"sort"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
)

type CostState struct {
	State     string  `json:"state"`
	Amount    *string `json:"amount"`
	Currency  string  `json:"currency"`
	Valuation string  `json:"valuation"`
	Sources   []Ref   `json:"sources"`
	// Stopping further starts cannot cap consumption by already running tools.
	Guarantee string `json:"guarantee"`
}

func (s *Service) cost(ctx context.Context, q query, p access.Principal, w, id string, b CostBudget) (CostState, error) {
	out := CostState{State: "observed", Currency: b.Currency, Valuation: b.Valuation, Sources: []Ref{}, Guarantee: "further_starts_only"}
	rows, err := q.Query(ctx, `SELECT t.run_id FROM workflow_tasks t JOIN shared_versions v ON v.workspace_id=t.workspace_id AND v.resource_id=t.run_id JOIN run_records r ON r.workspace_id=v.workspace_id AND r.id=v.resource_id AND r.version=v.version JOIN workflow_records d ON d.workspace_id=t.workspace_id AND d.id=t.payload->'definition'->>'id' AND d.version=t.payload->'definition'->>'version' WHERE t.workspace_id=$1 AND t.execution_id=$2 AND r.payload->>'started_at' IS NOT NULL AND d.payload->'ci' IS NULL`, w, id)
	if err != nil {
		return out, err
	}
	required := map[string]bool{}
	for rows.Next() {
		var run string
		if err = rows.Scan(&run); err != nil {
			rows.Close()
			return out, err
		}
		required[run] = false
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
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
			var deps []Ref
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
	out.Sources = unique(out.Sources)
	sort.Slice(out.Sources, func(i, j int) bool {
		if out.Sources[i].ID == out.Sources[j].ID {
			return out.Sources[i].Version < out.Sources[j].Version
		}
		return out.Sources[i].ID < out.Sources[j].ID
	})
	if err = s.checkRefs(ctx, q, p, w, out.Sources); err != nil {
		return out, err
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
