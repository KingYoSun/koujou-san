package mcpapi

import (
	"context"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) registerUsage(server *mcp.Server) {
	if s.Usage == nil {
		return
	}
	addTool(server, s, "usage_query", "Read authorized usage, tariffs, evaluations, period aggregates, export pages or changed ranges. Exactly one valuation kind per query. Does not create evaluations or execute work.", s.usageQuery)
	addTool(server, s, "usage_get", "Read one authorized fixed usage, price or evaluation version, including its authorized provenance.", s.usageGet)
	addTool(server, s, "price_set", "Save a manual tariff version with expected version and idempotent operation ID. Requires an explicitly created price-edit operation and current edit permission.", s.priceSet)
}
func (s *Service) usageQuery(ctx context.Context, r usage.Request) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Usage.Query(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v, Meta: v.Meta}, err
}
func (s *Service) usageGet(ctx context.Context, r usage.Ref) (outcome, error) {
	if r.Version == "" || r.Version == "head" {
		return outcome{}, access.ErrForbidden
	}
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Usage.Read(ctx, b.Principal, b.Workspace, r)
	kind, id, _ := strings.Cut(r.ID, ":")
	return outcome{Value: v, Refs: []contexts.Ref{{Kind: kind, ID: id, Revision: r.Version}}}, err
}

type priceWrite struct {
	ID    string      `json:"id"`
	Write usage.Write `json:"write"`
}

func (s *Service) priceSet(ctx context.Context, r priceWrite) (outcome, error) {
	if !strings.HasPrefix(r.ID, "price:") {
		return outcome{}, access.ErrForbidden
	}
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Usage.Put(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	refs := []contexts.Ref{}
	if v.Data != nil {
		refs = append(refs, contexts.Ref{Kind: "price", ID: strings.TrimPrefix(r.ID, "price:"), Revision: v.Data.Version})
	}
	return outcome{Value: v, Refs: refs, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
