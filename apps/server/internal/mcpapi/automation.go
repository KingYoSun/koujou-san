package mcpapi

import (
	"context"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) registerAutomation(server *mcp.Server) {
	if s.Automation == nil {
		return
	}
	addTool(server, s, "triage_get", "Read one authorized fixed policy, priority, activation or Worker-control version. Checkpoints and credentials are not exposed.", s.triageGet)
	addTool(server, s, "triage_query", "Read bounded policy/priority histories or processing state/history. Does not evaluate, publish or start a Run.", s.triageQuery)
	addTool(server, s, "triage_set", "Save policy/priority state with CAS and replay. Requires an explicit triage-edit or triage-manage operation. Activation and Worker authority require current Workspace administrator and triage-manage.", s.triageSet)
	addTool(server, s, "triage_apply_past", "Explicitly request one bounded page of past Issue targets for a fixed active policy. Requires triage-manage and current administrator permission.", s.triagePast)
	addTool(server, s, "triage_job_control", "Stop or explicitly resume accepted work, retaining history. Requires triage-manage and current administrator permission. Never starts a Run.", s.triageJob)
}
func (s *Service) triageGet(ctx context.Context, r automation.Ref) (outcome, error) {
	if r.Version == "" || r.Version == "head" {
		return outcome{}, access.ErrForbidden
	}
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Automation.Read(ctx, b.Principal, b.Workspace, r)
	k, id, _ := strings.Cut(r.ID, ":")
	return outcome{Value: v, Refs: []contexts.Ref{{Kind: k, ID: id, Revision: r.Version}}}, err
}
func (s *Service) triageQuery(ctx context.Context, r automation.Request) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Automation.Query(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v, Meta: v.Meta}, err
}

type triageWrite struct {
	ID    string           `json:"id"`
	Write automation.Write `json:"write"`
}

func (s *Service) triageSet(ctx context.Context, r triageWrite) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Automation.Put(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	refs := []contexts.Ref{}
	if v.Data != nil {
		k, id, _ := strings.Cut(v.Data.ID, ":")
		refs = append(refs, contexts.Ref{Kind: k, ID: id, Revision: v.Data.Version})
	}
	return outcome{Value: v, Refs: refs, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
func (s *Service) triagePast(ctx context.Context, r automation.ApplyPast) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Automation.ApplyPast(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.OperationID}, err
}

type triageJobRequest struct {
	ID    string               `json:"id"`
	Write automation.JobAction `json:"write"`
}

func (s *Service) triageJob(ctx context.Context, r triageJobRequest) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Automation.JobAction(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
