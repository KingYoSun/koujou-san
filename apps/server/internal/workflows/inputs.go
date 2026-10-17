package workflows

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

func replace(value json.RawMessage, path []string, next json.RawMessage) (json.RawMessage, error) {
	if len(path) == 0 {
		return next, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(value, &object) == nil && object != nil {
		v, err := replace(object[path[0]], path[1:], next)
		if err != nil {
			return nil, err
		}
		object[path[0]] = v
		return raw(object), nil
	}
	var array []json.RawMessage
	i, err := strconv.Atoi(path[0])
	if err != nil || json.Unmarshal(value, &array) != nil || i < 0 || i >= len(array) {
		return nil, ErrInvalid
	}
	array[i], err = replace(array[i], path[1:], next)
	return raw(array), err
}
func (s *Service) prepare(ctx context.Context, p access.Principal, w, id string, e Execution, n Node, ts map[string]Task) (Prepared, error) {
	d, r, err := s.definition(ctx, s.Store.Database(), p, w, *n.Definition)
	if err != nil {
		return Prepared{}, err
	}
	var spec runs.Spec
	_ = json.Unmarshal(raw(d.Spec), &spec)
	refs := append([]Ref{r.Ref}, r.Refs...)
	assign := func(name string, value json.RawMessage, path []string) error {
		switch name {
		case "context":
			if len(spec.Input.Context) == 0 {
				spec.Input.Context = value
			} else {
				spec.Input.Context = raw([]json.RawMessage{spec.Input.Context, value})
			}
		case "prompt":
			spec.Input.Prompt = value
		case "command":
			var err error
			spec.Input.Command.Parameters, err = replace(spec.Input.Command.Parameters, path, value)
			return err
		default:
			return ErrInvalid
		}
		return nil
	}
	for _, name := range n.Inputs {
		if err = assign(name, e.Inputs[n.ID][name], nil); err != nil {
			return Prepared{}, err
		}
	}
	for _, b := range n.Bindings {
		source := ts[b.From]
		if source.Run == nil || source.State != "done" {
			return Prepared{}, ErrState
		}
		var value json.RawMessage
		var ref Ref
		if b.Kind == "result" {
			record, err := s.Runs.Read(ctx, p, w, *source.Run)
			if err != nil {
				return Prepared{}, err
			}
			var run runs.Run
			_ = json.Unmarshal(record.Value, &run)
			if run.Assessment == nil {
				return Prepared{}, ErrState
			}
			value = run.Assessment.Report
			ref = record.Ref
		} else {
			// Index only the selected channel within the fixed completed Run. The other
			// channels and transcript are never loaded as implicit inputs.
			var payload []byte
			err = s.Store.Database().QueryRow(ctx, `SELECT id,version,payload FROM run_records WHERE workspace_id=$1 AND run_id=$2 AND kind='output' AND payload->>'channel'=$3 AND position <= (SELECT position FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$4) ORDER BY position LIMIT 1 OFFSET $5`, w, source.Run.ID, b.Channel, source.Run.Version, b.Index).Scan(&ref.ID, &ref.Version, &payload)
			if err != nil {
				return Prepared{}, err
			}
			if _, err = s.Runs.Read(ctx, p, w, ref); err != nil {
				return Prepared{}, err
			}
			var out runs.Output
			_ = json.Unmarshal(payload, &out)
			value = out.Value
		}
		value, err = selectValue(value, b.Path)
		if err != nil {
			return Prepared{}, err
		}
		if err = assign(b.To, value, b.Parameter); err != nil {
			return Prepared{}, err
		}
		refs = append(refs, ref)
	}
	out := Prepared{Spec: spec, Sources: refs, Judgment: spec.Derivation != nil || d.CI != nil}
	if d.Selection != nil {
		if d.Selection.Mode == "none" && len(spec.Input.Context) > 0 {
			return out, ErrInvalid
		}
		resolved := s.Contexts.Preview(ctx, p, w, contexts.ResolveRequest{RunID: id, Baseline: e.Baseline, Selection: *d.Selection})
		if !resolved.Ready {
			return out, ErrState
		}
		out.Manifest = resolved.Manifest
		if resolved.Manifest.InputUTF8 != nil {
			if err = assign("context", json.RawMessage(*resolved.Manifest.InputUTF8), nil); err != nil {
				return out, err
			}
		}
		for _, r := range resolved.Manifest.Placements {
			refs = append(refs, Ref{ID: "context:" + r.Ref.ID, Version: r.Ref.Revision})
		}
		if e.Baseline != nil {
			refs = append(refs, Ref{ID: "baseline:" + e.Baseline.ID, Version: e.Baseline.Revision})
		}
	}
	spec.Sources = unique(append(spec.Sources, refs...))
	out.Spec = spec
	out.Sources = spec.Sources
	if err = runs.ValidateSpec(spec); err != nil {
		return out, err
	}
	return out, s.checkRefs(ctx, s.Store.Database(), p, w, out.Sources)
}

type Preview struct {
	Execution Ref    `json:"execution"`
	Node      string `json:"node"`
	Format    string `json:"format,omitempty"`
}

func (s *Service) Preview(ctx context.Context, p access.Principal, w string, req Preview) (Prepared, error) {
	r, err := s.Read(ctx, p, w, req.Execution)
	if err != nil {
		return Prepared{}, err
	}
	var e Execution
	if r.Kind != "execution" || json.Unmarshal(r.Value, &e) != nil {
		return Prepared{}, ErrInvalid
	}
	// Only the current execution is previewed; task inputs already fixed at launch
	// remain available from their fixed task/Run records.
	current, err := record(ctx, s.Store.Database(), w, Ref{ID: r.ID, Version: "head"})
	if err != nil || current.Version != r.Version {
		return Prepared{}, ErrState
	}
	ts, err := tasks(ctx, s.Store.Database(), w, r.ID)
	if err != nil {
		return Prepared{}, err
	}
	t, ok := ts[req.Node]
	if !ok {
		return Prepared{}, ErrInvalid
	}
	if t.Prepared != nil {
		if err = s.checkRefs(ctx, s.Store.Database(), p, w, t.Inputs); err != nil {
			return Prepared{}, err
		}
		return *t.Prepared, nil
	}
	d, _, err := s.definition(ctx, s.Store.Database(), p, w, e.Definition)
	if err != nil {
		return Prepared{}, err
	}
	for _, n := range d.Nodes {
		if n.ID == req.Node && n.Definition != nil {
			return s.prepare(ctx, p, w, "preview", e, n, ts)
		}
	}
	return Prepared{}, ErrInvalid
}
