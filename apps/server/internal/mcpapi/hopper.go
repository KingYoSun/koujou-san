package mcpapi

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/hopper"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

func (s *Service) registerHopper(server *mcp.Server) {
	if s.Hopper == nil {
		return
	}
	addTool(server, s, "hopper_get", "Read one authorized fixed Hopper definition, evaluation or lineage record. Never evaluates or publishes.", s.hopperGet)
	addTool(server, s, "hopper_query", "Read a bounded page of current Hopper definitions, evaluations or history with current authorization. No evaluation or execution.", s.hopperQuery)
	addTool(server, s, "hopper_model_connections", "Read configured server model connection metadata for this Workspace. No credentials, inference or publication.", s.hopperConnections)
	addTool(server, s, "hopper_semantic_template", "Read editable initial semantic evaluation settings. Does not configure, evaluate or publish.", s.hopperSemanticTemplate)
	addTool(server, s, "hopper_set", "Save a fixed Hopper definition or activation with CAS/replay. Requires hopper-edit; activation requires hopper-manage and current administrator.", s.hopperSet)
	addTool(server, s, "hopper_evaluate_past", "Request evaluation of a bounded page of past accepted results. Default publish=false never publishes or starts Runs; semantic evaluation can consume model usage. Explicit publish=true enqueues normal evaluation and requires current active definition and automation authority. Requires hopper-manage.", s.hopperPast)
	addTool(server, s, "hopper_decide", "Explicitly publish a proposal or stop/resume a chain under current administrator, automation, template, execution and budget permissions. Dry-run results cannot be published.", s.hopperDecide)
}

func (s *Service) hopperConnections(ctx context.Context, _ struct{}) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Connections(ctx, b.Principal, b.Workspace)
	return outcome{Value: v}, err
}
func (s *Service) hopperSemanticTemplate(ctx context.Context, _ struct{}) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	err := s.Access.Check(ctx, b.Principal, b.Workspace, "", "read")
	return outcome{Value: hopper.DefaultSemantic()}, err
}
func (s *Service) hopperGet(ctx context.Context, r hopper.Ref) (outcome, error) {
	if r.Version == "" || r.Version == "head" {
		return outcome{}, access.ErrForbidden
	}
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Read(ctx, b.Principal, b.Workspace, r)
	k, id, _ := strings.Cut(r.ID, ":")
	return outcome{Value: v, Refs: []contexts.Ref{{Kind: k, ID: id, Revision: r.Version}}}, err
}
func (s *Service) hopperQuery(ctx context.Context, r hopper.Request) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Query(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v}, err
}

type hopperWrite struct {
	ID    string       `json:"id"`
	Write hopper.Write `json:"write"`
}

func (s *Service) hopperSet(ctx context.Context, r hopperWrite) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Put(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
func (s *Service) hopperPast(ctx context.Context, r hopper.Past) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Past(ctx, b.Principal, b.Workspace, r)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.OperationID}, err
}

type hopperDecision struct {
	ID    string          `json:"id"`
	Write hopper.Decision `json:"write"`
}

func (s *Service) hopperDecide(ctx context.Context, r hopperDecision) (outcome, error) {
	b := ctx.Value(operationKey{}).(boundary)
	v, err := s.Hopper.Decide(ctx, b.Principal, b.Workspace, r.ID, r.Write)
	return outcome{Value: v, Meta: v.Meta, MutationID: r.Write.OperationID}, err
}
