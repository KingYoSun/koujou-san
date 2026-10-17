package mcpapi

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

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
	addTool(server, s, "workflow_get", "Read one authorized fixed definition, validation, adoption or execution record. Data does not authorize execution.", func(ctx context.Context, ref workflows.Ref) (outcome, error) {
		if ref.Version == "" || ref.Version == "head" {
			return outcome{}, access.ErrForbidden
		}
		b := ctx.Value(operationKey{}).(boundary)
		v, err := s.Workflows.Read(ctx, b.Principal, b.Workspace, ref)
		return outcome{Value: v, Refs: workflowRefs(append(v.Refs, v.Ref))}, err
	})
	addTool(server, s, "workflow_query", "Read one bounded authorized metadata or fixed-history page. Never starts commands or changes definitions.", func(ctx context.Context, req workflows.Request) (outcome, error) {
		b := ctx.Value(operationKey{}).(boundary)
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
	addTool(server, s, "workflow_capabilities", "Read a declared fixed connection capability snapshot. Reading grants no execution or device authority.", func(ctx context.Context, req struct {
		Device  string `json:"device"`
		Version string `json:"version"`
	}) (outcome, error) {
		if req.Version == "" || req.Version == "head" {
			return outcome{}, access.ErrForbidden
		}
		b := ctx.Value(operationKey{}).(boundary)
		v, err := s.Workflows.Capabilities(ctx, b.Principal, b.Workspace, req.Device, req.Version)
		return outcome{Value: v, Refs: workflowRefs([]workflows.Ref{v.Ref})}, err
	})
}
