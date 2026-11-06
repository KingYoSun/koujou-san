package clientops

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
)

// Inspection starts from the operation's saved Project, never a model-supplied scope.
func (s *Service) ChatInspector(ctx context.Context, p access.Principal, w string) (Operation, ChatThread, access.Principal, error) {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found || op.Purpose != "chat" {
		return op, ChatThread{}, p, access.ErrForbidden
	}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), access.Principal{UserID: p.UserID}, w, op)
	if err != nil || policy.Chat == nil || !policy.Chat.Inspect || policy.Fixed.Mode == "none" {
		return op, ChatThread{}, p, access.ErrForbidden
	}
	candidate := access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}
	thread, _, err := s.chatThread(ctx, s.Store.Database(), candidate, w, policy.Chat.Thread, false)
	return op, thread, candidate, err
}
func (s *Service) InspectionRefs(ctx context.Context, p access.Principal, w string, op Operation, refs []Ref) error {
	if err := s.checkRefs(ctx, s.Store.Database(), access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}, w, refs); err != nil {
		return err
	}
	_, err := s.Store.Database().Exec(ctx, `INSERT INTO access_operation_refs SELECT $1,$2,r.id,r.version FROM jsonb_to_recordset($3::jsonb) r(id text,version text) ON CONFLICT DO NOTHING`, w, op.ReadOperation, raw(refs))
	return err
}
