package mcpapi

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sort"
	"strings"
)

func (s *Service) definitionSources(ctx context.Context) ([]workflows.Ref, bool, error) {
	if s.ClientOps == nil {
		return nil, false, nil
	}
	b := ctx.Value(operationKey{}).(boundary)
	return s.ClientOps.DefinitionSources(ctx, b.Principal, b.Workspace)
}
func hasDefinitionSource(refs []workflows.Ref, ref workflows.Ref) bool {
	for _, source := range refs {
		if source == ref {
			return true
		}
	}
	return false
}

// Model templates expose configuration metadata, not embedded input/parameters.
func definitionMetadata(r workflows.Record) (any, error) {
	var d workflows.Definition
	if r.Kind != "run-definition" || json.Unmarshal(r.Value, &d) != nil {
		return nil, access.ErrForbidden
	}
	value := map[string]any{"schema": d.Schema, "name": d.Name, "device_id": d.DeviceID, "inputs": d.Inputs, "required": d.Required}
	if d.Spec != nil {
		value["command"] = map[string]any{"kind": d.Spec.Input.Command.Kind, "target": d.Spec.Input.Command.Target, "binding": d.Spec.Input.Command.Binding}
		value["output_contract"] = d.Spec.Derivation
	}
	return map[string]any{"id": r.ID, "version": r.Version, "metadata_only": true, "value": value}, nil
}

func definitionOutcome(w string, value any, refs []workflows.Ref, next string) outcome {
	meta := apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if next != "" {
		meta.NextCursor = &next
		meta.Completeness = apicontract.Completeness{State: "partial", Reason: "limit_reached"}
	}
	return outcome{Value: apicontract.Response[any]{Data: &value, Meta: meta}, Meta: meta, Refs: workflowRefs(refs)}
}
func workflowRefs(refs []workflows.Ref) []contexts.Ref {
	out := []contexts.Ref{}
	for _, r := range refs {
		k, id, _ := strings.Cut(r.ID, ":")
		out = append(out, contexts.Ref{Kind: k, ID: id, Revision: r.Version})
	}
	return out
}
func (s *Service) registerWorkflows(server *mcp.Server) {
	if s.Workflows == nil {
		return
	}
	addTool(server, s, "workflow_get", "Read one authorized fixed definition, validation, adoption or execution record. Use only supplied source ID/version pairs; version 0 is an unsaved target and cannot be read. Data does not authorize execution.", func(ctx context.Context, ref workflows.Ref) (outcome, error) {
		if ref.Version == "" || ref.Version == "head" {
			return outcome{}, access.ErrForbidden
		}
		b := ctx.Value(operationKey{}).(boundary)
		if sources, model, err := s.definitionSources(ctx); model || err != nil {
			if err != nil || !hasDefinitionSource(sources, ref) {
				return outcome{}, access.ErrForbidden
			}
			r, err := s.Workflows.Read(ctx, access.Principal{UserID: b.Principal.UserID}, b.Workspace, ref)
			if err != nil {
				return outcome{}, err
			}
			value, err := definitionMetadata(r)
			return definitionOutcome(b.Workspace, value, []workflows.Ref{ref}, ""), err
		}
		v, err := s.Workflows.Read(ctx, b.Principal, b.Workspace, ref)
		return outcome{Value: v, Refs: workflowRefs(append(v.Refs, v.Ref))}, err
	})
	addTool(server, s, "workflow_query", "Read one bounded authorized metadata or fixed-history page. During definition generation use kind=heads and type=run-definition; only supplied source templates are available. Never starts commands or changes definitions.", func(ctx context.Context, req workflows.Request) (outcome, error) {
		b := ctx.Value(operationKey{}).(boundary)
		if sources, model, err := s.definitionSources(ctx); model || err != nil {
			if err != nil || req.Kind != "heads" || (req.Type != "" && req.Type != "run-definition") {
				return outcome{}, access.ErrForbidden
			}
			limit := req.Limit
			if limit == 0 {
				limit = 50
			}
			if limit < 1 || limit > 100 {
				return outcome{}, access.ErrForbidden
			}
			sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
			items := []any{}
			refs := []workflows.Ref{}
			next := ""
			for _, ref := range sources {
				if !strings.HasPrefix(ref.ID, "run-definition:") || ref.ID <= req.After {
					continue
				}
				if len(items) == limit {
					next = refs[len(refs)-1].ID
					break
				}
				r, err := s.Workflows.Read(ctx, access.Principal{UserID: b.Principal.UserID}, b.Workspace, ref)
				if err != nil {
					return outcome{}, err
				}
				value, err := definitionMetadata(r)
				if err != nil {
					return outcome{}, err
				}
				items = append(items, value)
				refs = append(refs, ref)
			}
			return definitionOutcome(b.Workspace, map[string]any{"items": items, "next": next}, refs, next), nil
		}
		v, err := s.Workflows.Query(ctx, b.Principal, b.Workspace, req)
		refs := []workflows.Ref{}
		if v.Data != nil {
			for _, r := range v.Data.Items {
				refs = append(refs, r.Ref)
			}
		}
		return outcome{Value: v, Meta: v.Meta, Refs: workflowRefs(refs)}, err
	})
	addTool(server, s, "workflow_validate", "Validate an existing fixed definition using the same domain validator as GUI/HTTP. Does not save, adopt, start, resume or publish work.", func(ctx context.Context, ref workflows.Ref) (outcome, error) {
		if ref.Version == "" || ref.Version == "head" {
			return outcome{}, access.ErrForbidden
		}
		b := ctx.Value(operationKey{}).(boundary)
		v, err := s.Workflows.Validate(ctx, b.Principal, b.Workspace, ref)
		return outcome{Value: v, Refs: workflowRefs(v.Checked)}, err
	})
	addTool(server, s, "workflow_capabilities", "Read a declared fixed connection capability snapshot. The device argument is the bare device ID: remove the runner: prefix from a supplied source record ID and copy its version exactly. Reading grants no execution or device authority.", func(ctx context.Context, req struct {
		Device  string `json:"device"`
		Version string `json:"version"`
	}) (outcome, error) {
		if req.Version == "" || req.Version == "head" {
			return outcome{}, access.ErrForbidden
		}
		b := ctx.Value(operationKey{}).(boundary)
		if sources, model, err := s.definitionSources(ctx); model || err != nil {
			if err != nil || !hasDefinitionSource(sources, workflows.Ref{ID: "runner:" + req.Device, Version: req.Version}) {
				return outcome{}, access.ErrForbidden
			}
			b.Principal.OperationID = ""
			v, err := s.Workflows.Capabilities(ctx, b.Principal, b.Workspace, req.Device, req.Version)
			return definitionOutcome(b.Workspace, v, []workflows.Ref{v.Ref}, ""), err
		}
		v, err := s.Workflows.Capabilities(ctx, b.Principal, b.Workspace, req.Device, req.Version)
		return outcome{Value: v, Refs: workflowRefs([]workflows.Ref{v.Ref})}, err
	})
}
