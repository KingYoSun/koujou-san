package usage

import (
	"encoding/json"
	"math/big"
	"reflect"
	"slices"
	"sort"
)

type Record struct {
	Ref
	Kind     string          `json:"kind"`
	Position int64           `json:"position"`
	Value    json.RawMessage `json:"value"`
}
type consumption struct {
	Observation
	Refs    []Ref
	Adopted map[string][]Ref
	State   string
}

// combine reads only the current series in one indexed coverage group. Original
// records, including suppressed children and corroborating sources, remain refs.
func combine(records []Record) (consumption, error) {
	out := consumption{State: "confirmed", Adopted: map[string][]Ref{}}
	requests := map[string]map[string][]Record{}
	covered := map[string]string{}
	for i, r := range records {
		var o Observation
		if json.Unmarshal(r.Value, &o) != nil || o.validate() != nil {
			return out, ErrInvalid
		}
		if i == 0 {
			out.Observation = o
			out.Quantities = map[string]Quantity{}
		} else if !reflect.DeepEqual(o.Attribution, out.Attribution) || o.Connection != out.Connection || o.Contract != out.Contract || !reflect.DeepEqual(o.ActualModel, out.ActualModel) {
			out.State = "review_needed"
		}
		out.Refs = append(out.Refs, r.Ref)
		if o.Coverage != "confirmed" {
			out.State = "review_needed"
		}
		if requests[o.Request] == nil {
			requests[o.Request] = map[string][]Record{}
		}
		requests[o.Request][o.Source] = append(requests[o.Request][o.Source], r)
		for _, child := range o.Covers {
			if child == o.Request || (covered[child] != "" && covered[child] != o.Request) {
				out.State = "review_needed"
			}
			covered[child] = o.Request
		}
	}
	for child := range covered {
		seen := map[string]bool{}
		for id := child; id != ""; id = covered[id] {
			if seen[id] {
				out.State = "review_needed"
				break
			}
			seen[id] = true
		}
	}
	for request, sources := range requests {
		if covered[request] != "" {
			continue
		}
		adopted := map[string]Quantity{}
		for _, source := range sortedKeys(sources) {
			summed := map[string]Quantity{}
			refs := map[string][]Ref{}
			for _, r := range sources[source] {
				var o Observation
				_ = json.Unmarshal(r.Value, &o)
				for k, q := range o.Quantities {
					prev, ok := summed[k]
					if ok {
						var err error
						q, err = addQuantity(prev, q)
						if err != nil {
							out.State = "review_needed"
							continue
						}
					}
					summed[k] = q
					refs[k] = append(refs[k], r.Ref)
				}
			}
			for k, q := range summed {
				prev, ok := adopted[k]
				if !ok || (prev.State == "missing" && q.State != "missing") {
					adopted[k] = q
					out.Adopted[request+":"+k] = refs[k]
				} else if q.State != "missing" && !sameQuantity(prev, q) {
					out.State = "review_needed"
				}
			}
		}
		for k, q := range adopted {
			if prev, ok := out.Quantities[k]; ok {
				var err error
				q, err = addQuantity(prev, q)
				if err != nil {
					out.State = "review_needed"
					continue
				}
			}
			out.Quantities[k] = q
		}
	}
	if _, _, err := disjoint(out.Quantities); err != nil {
		out.State = "review_needed"
	}
	sort.Slice(out.Refs, func(i, j int) bool { return out.Refs[i].ID < out.Refs[j].ID })
	return out, nil
}
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func sameQuantity(a, b Quantity) bool {
	if a.Unit != b.Unit || a.State != b.State || !slices.Equal(a.Includes, b.Includes) {
		return false
	}
	if a.Value == nil || b.Value == nil {
		return a.Value == nil && b.Value == nil
	}
	x, _ := number(*a.Value)
	y, _ := number(*b.Value)
	return x.Cmp(y) == 0
}
func addQuantity(a, b Quantity) (Quantity, error) {
	if a.Unit != b.Unit || !slices.Equal(a.Includes, b.Includes) || (a.State == "not_applicable") != (b.State == "not_applicable") {
		return Quantity{}, ErrInvalid
	}
	if a.State == "not_applicable" {
		return a, nil
	}
	if a.Value == nil || b.Value == nil {
		a.Value = nil
		a.State = "missing"
		return a, nil
	}
	x, _ := number(*a.Value)
	y, _ := number(*b.Value)
	v := exact(new(big.Rat).Add(x, y))
	a.Value = &v
	if a.State == "estimated" || b.State == "estimated" {
		a.State = "estimated"
	}
	return a, nil
}
