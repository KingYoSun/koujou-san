package inspections

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/jackc/pgx/v5"
)

type Start struct {
	apicontract.Mutation
	Adoption   Ref              `json:"adoption"`
	Device     string           `json:"device_id"`
	Repository *runs.Repository `json:"repository,omitempty"`
}

func (s *Service) prepare(ctx context.Context, p access.Principal, w, run string, req Start) (Launch, []Ref, error) {
	adopted, err := s.Read(ctx, p, w, req.Adoption)
	if err != nil {
		return Launch{}, nil, err
	}
	var a Adoption
	if adopted.Kind != "adoption" || decode(adopted.Value, &a) != nil || !fixed(a.Definition) {
		return Launch{}, nil, ErrInvalid
	}
	d, r, err := s.definition(ctx, s.Store.Database(), p, w, a.Definition)
	if err != nil {
		return Launch{}, nil, err
	}
	if req.Repository != nil {
		matches := false
		for _, target := range d.Targets {
			if target.Revision == req.Repository.Commit {
				matches = true
			}
		}
		if !matches {
			return Launch{}, nil, ErrInvalid
		}
	}
	template, err := s.Workflows.Read(ctx, p, w, d.Template)
	if err != nil {
		return Launch{}, nil, err
	}
	var definition workflows.Definition
	if decode(template.Value, &definition) != nil || definition.Spec == nil {
		return Launch{}, nil, ErrInvalid
	}
	var spec runs.Spec
	_ = json.Unmarshal(raw(definition.Spec), &spec)
	if spec.Manifest != nil || len(spec.Input.Context) > 0 || spec.Input.Command.Binding["prompt"] == "" {
		return Launch{}, nil, ErrInvalid
	}
	profile, err := s.Workflows.Capabilities(ctx, p, w, req.Device, "head")
	if err != nil {
		return Launch{}, nil, err
	}
	var connection *runs.Connection
	for _, c := range profile.Connections {
		if c.Kind == spec.Input.Command.Kind && c.Target == spec.Input.Command.Target {
			copy := c
			connection = &copy
			break
		}
	}
	if connection == nil {
		return Launch{}, nil, ErrState
	}
	actual, err := s.separation(ctx, s.Store.Database(), p, w, d, connection.Capabilities)
	if err != nil {
		return Launch{}, nil, err
	}
	if len(gaps(d.Required, actual)) > 0 {
		return Launch{}, nil, ErrState
	}
	resolved := s.Contexts.Preview(ctx, p, w, contexts.ResolveRequest{RunID: run, Selection: d.Selection})
	if !resolved.Ready {
		return Launch{}, nil, ErrState
	}
	manifest := *resolved.Manifest
	manifest.AdoptedBy = p.UserID
	if manifest.InputUTF8 != nil {
		spec.Input.Context = json.RawMessage(*manifest.InputUTF8)
	}
	field := "verdict"
	if spec.Derivation != nil && spec.Derivation.VerdictField != "" {
		field = spec.Derivation.VerdictField
	}
	request := map[string]any{"inspection": a.Definition, "targets": d.Targets, "criteria": d.Criteria, "manifest_digest": manifest.Digest, "context_mode": d.Selection.Mode, "required_separation": d.Required, "separation": actual, "result_format": "inspection-report-v1", "verdict_field": field}
	if len(spec.Input.Prompt) > 0 {
		request["request"] = spec.Input.Prompt
	}
	if d.Reuse != nil {
		request["reuse"] = d.Reuse
		spec.Previous = &d.Reuse.Run
	}
	spec.Input.Prompt = raw(request)
	spec.Artifacts = append(spec.Artifacts, d.Targets...)
	if spec.Derivation == nil {
		spec.Derivation = &runs.Contract{Name: d.Kind, Version: "inspection-report-v1"}
	}
	spec.Derivation.VerdictField = field
	refs := append(append(append([]Ref{}, r.Refs...), adopted.Refs...), r.Ref, adopted.Ref, template.Ref, profile.Ref)
	for _, placement := range manifest.Placements {
		refs = append(refs, Ref{ID: "context:" + placement.Ref.ID, Version: placement.Ref.Revision})
	}
	spec.Sources = unique(append(spec.Sources, refs...))
	if err = runs.ValidateSpec(spec); err != nil {
		return Launch{}, nil, err
	}
	launch := Launch{Definition: a.Definition, Template: d.Template, Run: run, Manifest: manifest, Spec: spec, Separation: actual, Device: req.Device, Repository: req.Repository, By: p.UserID, At: time.Now().UTC()}
	return launch, unique(spec.Sources), nil
}
func accepted(value apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if value.Error != nil || value.Data == nil {
		return Ref{}, ErrState
	}
	return Ref{ID: value.Data.ID, Version: value.Data.Version}, nil
}
func (s *Service) Start(ctx context.Context, p access.Principal, w, run string, req Start) (apicontract.Response[apicontract.Reference], error) {
	run = "run:" + strings.TrimPrefix(run, "run:")
	if run == "run:" || req.ExpectedVersion != "0" || req.Device == "" || !fixed(req.Adoption) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	// Definition/launch creation uses the same edit permission as public Run
	// creation. Execute is checked separately before any Run can be assigned.
	if err := s.Access.Check(ctx, p, w, "", "register"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	a, err := s.Read(ctx, p, w, req.Adoption)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	id := "inspection-launch:" + strings.TrimPrefix(run, "run:")
	response, err := s.mutate(ctx, p, w, id, a.Owner, "launch", "execute", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		launch, refs, err := s.prepare(ctx, p, w, run, req)
		if err != nil {
			return Record{}, err
		}
		launch.Spec.Sources = unique(append(refs, ref))
		if err = s.refs(ctx, tx, p, w, refs); err != nil {
			return Record{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inspection_launches VALUES($1,$2,$3,$4)`, w, run, ref.ID, ref.Version); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, a.Owner, "launch", launch, refs)
	})
	launchRef, err := accepted(response, err)
	if err != nil {
		return response, err
	}
	saved, err := s.Read(ctx, p, w, launchRef)
	if err != nil {
		return response, err
	}
	var launch Launch
	if decode(saved.Value, &launch) != nil {
		return response, ErrInvalid
	}
	created, err := s.Runs.Create(ctx, p, w, run, runs.Create{Mutation: apicontract.Mutation{OperationID: id + ":create", ExpectedVersion: "0"}, Spec: launch.Spec})
	if _, err = accepted(created, err); err != nil {
		return response, err
	}
	current, err := s.Runs.Read(ctx, p, w, Ref{ID: run, Version: "head"})
	if err != nil {
		return response, err
	}
	var state runs.Run
	_ = json.Unmarshal(current.Value, &state)
	if state.Assignment == nil {
		_, err = accepted(s.Runs.Assign(ctx, p, w, run, runs.Assign{Mutation: apicontract.Mutation{OperationID: id + ":assign:" + current.Version, ExpectedVersion: current.Version}, DeviceID: req.Device, Repository: req.Repository}))
	}
	return response, err
}
