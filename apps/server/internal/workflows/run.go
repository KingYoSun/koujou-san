package workflows

import (
	"context"
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

// CreateRun materializes a fixed adopted definition without assigning or starting it.
type CreateRun struct {
	apicontract.Mutation
	Adoption   Ref    `json:"adoption"`
	InputsUTF8 string `json:"inputs_utf8"`
}

func (s *Service) CreateRun(ctx context.Context, p access.Principal, w, id string, req CreateRun) (apicontract.Response[apicontract.Reference], error) {
	empty := apicontract.Response[apicontract.Reference]{}
	if p.OperationID != "" {
		return empty, access.ErrForbidden
	}
	if req.ExpectedVersion != "0" {
		return empty, ErrInvalid
	}
	d, refs, err := s.adoptedDefinition(ctx, s.database(), p, w, req.Adoption, "run-definition-v1")
	if err != nil {
		return empty, err
	}
	// CI observations and guarded writes belong to the existing Workflow controller.
	if d.CI != nil || d.Write != nil {
		return empty, ErrInvalid
	}
	inputs := map[string]json.RawMessage{}
	if decode([]byte(req.InputsUTF8), &inputs) != nil || inputs == nil || len(inputs) != len(d.Inputs) {
		return empty, ErrInvalid
	}
	for _, name := range d.Inputs {
		if _, ok := inputs[name]; !ok {
			return empty, ErrInvalid
		}
	}
	adopted, err := s.Read(ctx, p, w, req.Adoption)
	if err != nil {
		return empty, err
	}
	var a Adoption
	if err = json.Unmarshal(adopted.Value, &a); err != nil {
		return empty, err
	}
	prepared, err := s.prepare(ctx, p, w, id, Execution{Baseline: d.Baseline, Inputs: map[string]map[string]json.RawMessage{"run": inputs}}, Node{ID: "run", Definition: &a.Definition, Inputs: d.Inputs}, nil)
	if err != nil {
		return empty, err
	}
	prepared.Spec.Sources = unique(append(prepared.Spec.Sources, refs...))
	return s.Runs.Create(ctx, p, w, id, runs.Create{Mutation: req.Mutation, Spec: prepared.Spec})
}
