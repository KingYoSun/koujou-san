package runs

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

// Connector operations use the same immutable input and Run records. These are
// trusted server methods, never routes that accept invented CLI/device events.
func (s *Service) CreateForConnector(ctx context.Context, p access.Principal, w, id string, req Create) (apicontract.Response[apicontract.Reference], error) {
	if s.BeforeStart == nil || req.Spec.Input.Command.Kind != "server-connector" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.create(ctx, p, w, id, req, "connector-create", nil)
}
func (s *Service) RecordConnector(ctx context.Context, w, id string, source Ref, m apicontract.Mutation, started bool, report json.RawMessage, exit int) (apicontract.Response[apicontract.Reference], error) {
	id = runID(id)
	scope := persistence.Scope{ActorID: "connector:" + id, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}}
	payload := struct {
		Source  Ref
		Started bool
		Report  json.RawMessage
		Exit    int
	}{source, started, report, exit}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "connector-observation", Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, old, err := loadRun(ctx, tx, w, id)
		if err != nil {
			return nil, err
		}
		spec, err := s.input(ctx, tx, w, r)
		if err != nil {
			return nil, err
		}
		found := false
		for _, candidate := range spec.Sources {
			if candidate == source {
				found = true
			}
		}
		if spec.Input.Command.Kind != "server-connector" || !found || r.Assignment != nil {
			return nil, ErrInvalid
		}
		now := time.Now().UTC()
		if started {
			if r.State != "queued" {
				return nil, ErrState
			}
			if s.BeforeStart == nil {
				return nil, ErrState
			}
			if err = s.BeforeStart(ctx, tx, w, id); err != nil {
				return nil, err
			}
			r.State = "running"
			r.StartedAt = &now
			r.ExecutionRequestedAt = &now
			r.ReportedThrough = "1"
			r.Execution = &ExecutionInfo{Environment: "server-connector", Version: "github-issues-v1", VersionObserved: true}
		} else {
			if r.State != "running" || !json.Valid(report) {
				return nil, ErrState
			}
			r.State = "succeeded"
			if exit != 0 {
				r.State = "failed"
			}
			r.ExitCode = &exit
			r.FinishedAt = &now
			r.ReportedThrough = "2"
			r.Assessment = assess(spec.Derivation, report)
			output := Output{Sequence: "2", Channel: "result", Source: "server-connector", Value: report, At: now}
			if err = s.save(ctx, tx, w, id, "run-output:"+strings.TrimPrefix(id, "run:")+":2", "output", ref.Version, output, old.Refs); err != nil {
				return nil, err
			}
			if err = s.save(ctx, tx, w, id, "run-result:"+strings.TrimPrefix(id, "run:"), "result", ref.Version, r, old.Refs); err != nil {
				return nil, err
			}
		}
		if err = s.save(ctx, tx, w, id, id, "run", ref.Version, r, old.Refs); err != nil {
			return nil, err
		}
		return raw(Record{Ref: Ref{ID: id, Version: ref.Version}, RunID: id, Kind: "run", Value: raw(r), Refs: old.Refs}), nil
	})
}
