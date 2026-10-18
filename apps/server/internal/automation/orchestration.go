package automation

import (
	"context"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/jackc/pgx/v5"
)

// CheckJobTx reuses the Worker lease, current authority and reserved budget.
// Orchestrators call it again in the transaction that commits domain effects.
func (s *Service) CheckJobTx(ctx context.Context, tx pgx.Tx, j Job) (Control, Ref, error) {
	if err := sameLease(ctx, tx, j); err != nil {
		return Control{}, Ref{}, err
	}
	c, err := s.authority(ctx, tx, j, true)
	if err != nil {
		return c, Ref{}, err
	}
	reason, err := s.budget(ctx, tx, j, c, true)
	if err != nil {
		return c, Ref{}, err
	}
	if reason != "" {
		return c, Ref{}, access.ErrForbidden
	}
	if j.Kind == "triage" {
		if err = s.checkBatch(ctx, tx, j, access.Principal{UserID: c.Controller}, true); err != nil {
			return c, Ref{}, err
		}
	}
	r, err := record(ctx, tx, j.Workspace, Ref{ID: "control:" + j.Kind, Version: "head"})
	return c, r.Ref, err
}
func (s *Service) Hold(ctx context.Context, j Job, reason string) error {
	return s.block(ctx, j, reason)
}
func (s *Service) CheckManualTx(ctx context.Context, tx pgx.Tx, w string, refs []Ref) (Control, Ref, error) {
	j := Job{Workspace: w, Kind: "hopper", Refs: refs}
	c, err := s.controller(ctx, tx, j, true)
	if err != nil {
		return c, Ref{}, err
	}
	reason, err := s.budget(ctx, tx, j, c, true)
	if err != nil {
		return c, Ref{}, err
	}
	if reason != "" {
		return c, Ref{}, access.ErrForbidden
	}
	r, err := record(ctx, tx, w, Ref{ID: "control:hopper", Version: "head"})
	return c, r.Ref, err
}
