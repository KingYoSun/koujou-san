package hopper

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

func selected(v any, path []string) (any, bool) {
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return v, v != nil
}
func decodeValue(b []byte) any {
	var v any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	_ = d.Decode(&v)
	return v
}
func field(f Facts, path string) (any, bool) {
	switch path {
	case "run.state":
		return f.State, f.State != ""
	case "run.exit_code":
		if f.ExitCode != nil {
			return json.Number(string(raw(*f.ExitCode))), true
		}
	case "inspection.verdict":
		return f.Verdict, f.Verdict != "" && f.Verdict != "INCONCLUSIVE"
	case "ci.conclusion":
		switch f.Conclusion {
		case "success", "failure", "cancelled", "timed_out", "neutral", "skipped", "action_required", "stale":
			return f.Conclusion, true
		}
	default:
		if strings.HasPrefix(path, "result.") {
			return selected(decodeValue(f.Report), strings.Split(strings.TrimPrefix(path, "result."), "."))
		}
	}
	return nil, false
}
func validCondition(c Condition) bool {
	branches := 0
	for _, children := range [][]Condition{c.All, c.Any} {
		if len(children) > 0 {
			branches++
			for _, child := range children {
				if !validCondition(child) {
					return false
				}
			}
		}
	}
	if c.Not != nil {
		branches++
		if !validCondition(*c.Not) {
			return false
		}
	}
	if c.Field != "" {
		branches++
		if c.Op != "eq" && c.Op != "in" {
			return false
		}
		if !json.Valid(c.Value) {
			return false
		}
		if c.Op == "in" {
			if _, ok := decodeValue(c.Value).([]any); !ok {
				return false
			}
		}
		switch c.Field {
		case "run.state", "run.exit_code", "inspection.verdict", "ci.conclusion":
		default:
			if !strings.HasPrefix(c.Field, "result.") {
				return false
			}
		}
	}
	return branches == 1
}

// Three-valued ordered evaluation: false dominates AND, true dominates OR.
// Unknown evidence is never silently converted to a negative decision.
func Evaluate(c Condition, f Facts) string {
	if len(c.All) > 0 || len(c.Any) > 0 {
		children, stop, result := c.All, "false", "true"
		if len(c.Any) > 0 {
			children, stop, result = c.Any, "true", "false"
		}
		unknown := false
		for _, child := range children {
			v := Evaluate(child, f)
			if v == stop {
				return stop
			}
			unknown = unknown || v == "unknown"
		}
		if unknown {
			return "unknown"
		}
		return result
	}
	if c.Not != nil {
		switch Evaluate(*c.Not, f) {
		case "true":
			return "false"
		case "false":
			return "true"
		}
		return "unknown"
	}
	v, ok := field(f, c.Field)
	if !ok {
		return "unknown"
	}
	match := false
	switch c.Op {
	case "eq":
		match = reflect.DeepEqual(v, decodeValue(c.Value))
	case "in":
		for _, candidate := range decodeValue(c.Value).([]any) {
			match = match || reflect.DeepEqual(v, candidate)
		}
	}
	if match {
		return "true"
	}
	return "false"
}
