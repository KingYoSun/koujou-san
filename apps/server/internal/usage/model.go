// Package usage owns observed consumption, immutable tariff versions and cost
// evaluations. Decimal strings cross every boundary without binary floats.
package usage

import (
	"errors"
	"math/big"
	"regexp"
	"sort"
	"time"
)

var ErrInvalid = errors.New("invalid_usage")
var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

type Quantity struct {
	Value    *string  `json:"value"`
	State    string   `json:"state"` // reported, estimated, missing, not_applicable
	Unit     string   `json:"unit"`
	Includes []string `json:"includes,omitempty"`
}
type Attribution struct {
	Kind     string   `json:"kind"` // run, operation, hopper, triage, unattributed
	ID       string   `json:"id,omitempty"`
	Workflow string   `json:"workflow,omitempty"`
	Scopes   []string `json:"scopes"`
	Resource string   `json:"resource,omitempty"` // actual protected case, never Context grouping
}
type Observation struct {
	Source         string              `json:"source"`
	Transform      string              `json:"transform_version"`
	Event          string              `json:"event_id"`
	Series         string              `json:"series_id"`
	Sequence       int64               `json:"sequence"`
	Mode           string              `json:"mode"` // delta or cumulative
	Group          string              `json:"coverage_group"`
	Request        string              `json:"external_request_id"`
	Covers         []string            `json:"covers_requests"`
	Coverage       string              `json:"coverage"` // confirmed or unknown
	Connection     string              `json:"connection"`
	Contract       string              `json:"contract,omitempty"`
	RequestedModel string              `json:"requested_model,omitempty"`
	ActualModel    *string             `json:"actual_model"`
	At             time.Time           `json:"at"`
	Attribution    Attribution         `json:"attribution"`
	Quantities     map[string]Quantity `json:"quantities"`
}
type Rate struct {
	Amount string `json:"amount"`
	Per    string `json:"per"`
	Unit   string `json:"unit"`
}
type Price struct {
	Connection string          `json:"connection"`
	Contract   string          `json:"contract,omitempty"`
	Model      string          `json:"model"`
	Currency   string          `json:"currency"`
	From       time.Time       `json:"effective_from"`
	Until      *time.Time      `json:"effective_until"`
	Source     string          `json:"source"`
	Scale      int             `json:"scale"`
	Rounding   string          `json:"rounding"`
	Rates      map[string]Rate `json:"rates"`
}
type Ref struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Evaluation struct {
	Group      string              `json:"coverage_group"`
	Kind       string              `json:"kind"` // reported, estimated, api_equivalent, allocated
	Usage      []Ref               `json:"usage"`
	Price      *Ref                `json:"price"`
	Previous   *Ref                `json:"previous,omitempty"`
	Amount     *string             `json:"amount"`
	Currency   string              `json:"currency"`
	Source     string              `json:"source"`
	Scale      int                 `json:"scale"`
	Rounding   string              `json:"rounding"`
	FX         *Exchange           `json:"fx,omitempty"`
	State      string              `json:"state"`
	Adopted    map[string][]Ref    `json:"adopted"`
	Quantities map[string]Quantity `json:"quantities"`
}
type Exchange struct {
	OriginalAmount *string   `json:"original_amount"`
	Rate           string    `json:"rate"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	At             time.Time `json:"at"`
	Source         string    `json:"source"`
}

func number(s string) (*big.Rat, error) {
	if !decimal.MatchString(s) {
		return nil, ErrInvalid
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, ErrInvalid
	}
	return v, nil
}
func round(v *big.Rat, scale int, rule string) (string, error) {
	if scale < 0 || scale > 30 || (rule != "half_up" && rule != "half_even" && rule != "down") || v.Sign() < 0 {
		return "", ErrInvalid
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	n := new(big.Int).Mul(v.Num(), pow)
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, v.Denom(), r)
	cmp := new(big.Int).Lsh(r, 1).Cmp(v.Denom())
	if rule != "down" && (cmp > 0 || (cmp == 0 && (rule == "half_up" || q.Bit(0) == 1))) {
		q.Add(q, big.NewInt(1))
	}
	return new(big.Rat).SetFrac(q, pow).FloatString(scale), nil
}
func validQuantity(q Quantity) bool {
	if q.Unit == "" {
		return false
	}
	if q.State == "missing" || q.State == "not_applicable" {
		return q.Value == nil
	}
	if q.State != "reported" && q.State != "estimated" {
		return false
	}
	if q.Value == nil {
		return false
	}
	_, err := number(*q.Value)
	return err == nil
}
func (o Observation) validate() error {
	if o.Source == "" || o.Transform == "" || o.Event == "" || o.Series == "" || o.Sequence < 0 || o.Group == "" || o.Connection == "" || o.At.IsZero() || (o.Mode != "delta" && o.Mode != "cumulative") || (o.Coverage != "confirmed" && o.Coverage != "unknown") {
		return ErrInvalid
	}
	if o.Coverage == "confirmed" && o.Request == "" {
		return ErrInvalid
	}
	if o.ActualModel != nil && (*o.ActualModel == "" || *o.ActualModel == "auto") {
		return ErrInvalid
	}
	switch o.Attribution.Kind {
	case "run", "operation", "hopper", "triage":
		if o.Attribution.ID == "" {
			return ErrInvalid
		}
	case "unattributed":
		if o.Attribution.ID != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if len(o.Quantities) == 0 {
		return ErrInvalid
	}
	for k, q := range o.Quantities {
		if k == "" || !validQuantity(q) {
			return ErrInvalid
		}
	}
	_, _, err := disjoint(o.Quantities)
	return err
}
func (p Price) validate() error {
	if p.Connection == "" || p.Model == "" || p.Currency == "" || p.Source == "" || p.From.IsZero() || (p.Until != nil && !p.Until.After(p.From)) || len(p.Rates) == 0 {
		return ErrInvalid
	}
	if _, err := round(new(big.Rat), p.Scale, p.Rounding); err != nil {
		return err
	}
	for k, r := range p.Rates {
		if k == "" || r.Unit == "" {
			return ErrInvalid
		}
		if _, e := number(r.Amount); e != nil {
			return e
		}
		n, e := number(r.Per)
		if e != nil || n.Sign() == 0 {
			return ErrInvalid
		}
	}
	return nil
}

// disjoint partitions inclusive totals into nonoverlapping quantities. It also
// returns each child's original parent for a tariff that charges both equally.
func disjoint(qs map[string]Quantity) (map[string]Quantity, map[string]string, error) {
	out := map[string]Quantity{}
	parents := map[string]string{}
	for k, q := range qs {
		out[k] = q
		for _, child := range q.Includes {
			c, ok := qs[child]
			if !ok || child == k || c.Unit != q.Unit || parents[child] != "" {
				return nil, nil, ErrInvalid
			}
			parents[child] = k
		}
	}
	for k := range qs {
		seen := map[string]bool{}
		for p := k; p != ""; p = parents[p] {
			if seen[p] {
				return nil, nil, ErrInvalid
			}
			seen[p] = true
		}
	}
	for k, q := range qs {
		q.Includes = nil
		if q.Value != nil {
			v, _ := number(*q.Value)
			for _, child := range qs[k].Includes {
				c := qs[child]
				if c.Value == nil {
					continue
				}
				n, _ := number(*c.Value)
				v.Sub(v, n)
			}
			if v.Sign() < 0 {
				return nil, nil, ErrInvalid
			}
			s := exact(v)
			q.Value = &s
		}
		out[k] = q
	}
	return out, parents, nil
}
func exact(v *big.Rat) string {
	// Input decimals, subtraction and sums have finite decimal expansions.
	d := new(big.Int).Set(v.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	a, b := 0, 0
	for new(big.Int).Mod(d, two).Sign() == 0 {
		d.Div(d, two)
		a++
	}
	for new(big.Int).Mod(d, five).Sign() == 0 {
		d.Div(d, five)
		b++
	}
	if b > a {
		a = b
	}
	return v.FloatString(a)
}
func calculate(qs map[string]Quantity, p Price) (*string, string, error) {
	parts, parents, err := disjoint(qs)
	if err != nil {
		return nil, "", err
	}
	total := new(big.Rat)
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q := parts[k]
		if q.State == "not_applicable" {
			continue
		}
		r, ok := p.Rates[k]
		for parent := parents[k]; !ok && parent != ""; parent = parents[parent] {
			r, ok = p.Rates[parent]
		}
		if !ok || q.Value == nil || q.Unit != r.Unit {
			return nil, "unrated", nil
		}
		v, e := number(*q.Value)
		if e != nil {
			return nil, "", e
		}
		rate, _ := number(r.Amount)
		per, _ := number(r.Per)
		total.Add(total, new(big.Rat).Quo(new(big.Rat).Mul(v, rate), per))
	}
	value, err := round(total, p.Scale, p.Rounding)
	return &value, "rated", err
}
