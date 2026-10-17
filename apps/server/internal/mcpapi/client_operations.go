package mcpapi

import (
	"context"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) registerClientOperations(server *mcp.Server) {
	if s.ClientOps == nil {
		return
	}
	addTool(server, s, "selection_get", "Read a fixed Context selection, policy, client operation, proposal or raw response with current provenance permissions. Does not generate or execute.", s.selectionGet)
	addTool(server, s, "selection_query", "Read one authorized page of selection and client operation history, optionally within one operation.", s.selectionQuery)
	addTool(server, s, "selection_policy_set", "Save a fixed selection policy using CAS/replay. Requires selection-edit. Does not start a model or a Run.", s.selectionPolicy)
	addTool(server, s, "selection_set", "Apply an explicit selection through the common resolver, immutable Manifest and CAS/replay. Requires selection-edit. Never starts a Run.", s.selectionSet)
}
func versionRefs(refs []clientops.Ref) []contexts.Ref {
	out := []contexts.Ref{}
	for _, r := range refs {
		k, id, _ := strings.Cut(r.ID, ":")
		out = append(out, contexts.Ref{Kind: k, ID: id, Revision: r.Version})
	}
	return out
}
func (s *Service) selectionGet(ctx context.Context, r clientops.Ref) (outcome, error) {
	if r.Version == "" || r.Version == "head" {
		return outcome{}, access.ErrForbidden
	}
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.ClientOps.Read(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v, Refs: versionRefs(append(append([]clientops.Ref{}, v.Refs...), v.Ref))}, err
}
func (s *Service) selectionQuery(ctx context.Context, r clientops.Request) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.ClientOps.Query(ctx, b.Principal, b.Workspace, r)
	refs := []clientops.Ref{}
	for _, item := range v.Items {
		refs = append(refs, item.Ref)
		refs = append(refs, item.Refs...)
		refs = append(refs, item.Dependencies...)
	}
	return outcome{Value: v, Refs: versionRefs(refs)}, err
}

type policyWrite struct {
	ID    string          `json:"id"`
	Write clientops.Write `json:"write"`
}

func (s *Service) selectionPolicy(ctx context.Context, r policyWrite) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.ClientOps.PutPolicy(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}

type selectionWrite struct {
	ID    string                   `json:"id"`
	Write clientops.SelectionWrite `json:"write"`
}

func (s *Service) selectionSet(ctx context.Context, r selectionWrite) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.ClientOps.PutSelection(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
