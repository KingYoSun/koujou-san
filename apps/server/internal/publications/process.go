package publications

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func (s *Service) change(ctx context.Context, w string, old Record, phase string, apply func(pgx.Tx, Ref) (Record, error)) (Record, bool, error) {
	ran := false
	scope := persistence.Scope{ActorID: "publication-server", Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}}
	result, err := s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: old.ID, Kind: "publication-" + phase, Mutation: apicontract.Mutation{OperationID: old.ID + ":" + phase + ":" + old.Version, ExpectedVersion: old.Version}, Payload: raw(old.Ref)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		r, err := apply(tx, Ref{ID: ref.ID, Version: ref.Version})
		if err == nil {
			ran = true
		}
		return raw(r), err
	})
	if err != nil {
		return Record{}, false, err
	}
	if result.Error != nil || result.Data == nil {
		return Record{}, false, ErrState
	}
	r, err := record(ctx, s.Store.Database(), w, Ref{ID: result.Data.ID, Version: result.Data.Version})
	return r, ran, err
}
func (s *Service) observe(ctx context.Context, w string, old Record, result Outcome, finishing bool) (Record, error) {
	r, _, err := s.change(ctx, w, old, "observe", func(tx pgx.Tx, ref Ref) (Record, error) {
		var request Request
		if decode(old.Value, &request) != nil {
			return Record{}, ErrInvalid
		}
		request.State = result.State
		request.Reason = result.Reason
		request.ObservedAt = &result.At
		if result.Issue != nil {
			request.External = result.Issue
		}
		if _, err := s.save(ctx, tx, w, Ref{ID: "publication-observation:" + strings.TrimPrefix(old.ID, "publication:") + ":" + ref.Version, Version: ref.Version}, old.Owner, "observation", result, old.Refs); err != nil {
			return Record{}, err
		}
		state := result.State
		if finishing {
			state = "finishing"
		}
		if _, err := tx.Exec(ctx, `UPDATE publication_queue SET state=$3,due_at=now()+interval '60 seconds' WHERE workspace_id=$1 AND id=$2`, w, old.ID, state); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, old.Owner, "request", request, old.Refs)
	})
	return r, err
}
func accepted(v apicontract.Response[apicontract.Reference], err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if v.Error != nil || v.Data == nil {
		return Ref{}, ErrState
	}
	return Ref{ID: v.Data.ID, Version: v.Data.Version}, nil
}
func (s *Service) finishRun(ctx context.Context, w string, rec Record) error {
	var request Request
	if decode(rec.Value, &request) != nil {
		return ErrInvalid
	}
	var b []byte
	err := s.Store.Database().QueryRow(ctx, `SELECT payload FROM run_records WHERE workspace_id=$1 AND id=$2 AND kind='run' ORDER BY run_records.position DESC LIMIT 1`, w, request.Run).Scan(&b)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var state runs.Run
	_ = json.Unmarshal(b, &state)
	if state.State == "running" {
		current, err := s.Runs.Read(ctx, access.Principal{UserID: request.Actor.User}, w, Ref{ID: request.Run, Version: "head"})
		// Recording an already-issued operation is a trusted observation. A later
		// revocation stops new sends; it cannot erase an observed external side effect.
		if err != nil {
			var version string
			if err = s.Store.Database().QueryRow(ctx, `SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2`, w, request.Run).Scan(&version); err != nil {
				return err
			}
			current.Ref = Ref{ID: request.Run, Version: version}
		}
		exit := 0
		if request.State == "unknown" {
			exit = 1
		}
		report := Outcome{State: request.State, Reason: request.Reason, Issue: request.External}
		if request.ObservedAt != nil {
			report.At = *request.ObservedAt
		}
		if _, err = accepted(s.Runs.RecordConnector(ctx, w, request.Run, request.Accepted, apicontract.Mutation{OperationID: request.Run + ":finish", ExpectedVersion: current.Version}, false, raw(report), exit)); err != nil {
			return err
		}
	}
	_, err = s.Store.Database().Exec(ctx, `UPDATE publication_queue SET state=$3,due_at=now()+interval '60 seconds' WHERE workspace_id=$1 AND id=$2 AND state='finishing'`, w, rec.ID, request.State)
	return err
}
func (s *Service) Process(ctx context.Context, w, id string) error {
	rec, err := record(ctx, s.Store.Database(), w, Ref{ID: id, Version: "head"})
	if err != nil {
		return err
	}
	var request Request
	if rec.Kind != "request" || decode(rec.Value, &request) != nil {
		return ErrInvalid
	}
	var phase string
	var due time.Time
	if err = s.Store.Database().QueryRow(ctx, `SELECT state,due_at FROM publication_queue WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&phase, &due); err != nil {
		return err
	}
	if phase == "finishing" {
		return s.finishRun(ctx, w, rec)
	}
	if request.State == "sending" {
		if time.Now().Before(due) {
			return nil
		}
		rec, err = s.observe(ctx, w, rec, outcome("unknown", "sending_process_unconfirmed", nil), true)
		if err != nil {
			return err
		}
		return s.finishRun(ctx, w, rec)
	}
	if request.State == "unknown" {
		if err = s.authority(ctx, s.Store.Database(), w, request, rec.Owner, false); err != nil {
			return err
		}
		_, err = s.refresh(ctx, w, rec)
		return err
	}
	if request.State != "accepted" {
		return nil
	}
	if err = s.authority(ctx, s.Store.Database(), w, request, rec.Owner, true); err != nil {
		_, _, saveErr := s.change(ctx, w, rec, "hold", func(tx pgx.Tx, ref Ref) (Record, error) {
			request.State = "held"
			request.Reason = "authority_or_basis_changed"
			if _, err := tx.Exec(ctx, `UPDATE publication_queue SET state='held' WHERE workspace_id=$1 AND id=$2`, w, id); err != nil {
				return Record{}, err
			}
			return s.save(ctx, tx, w, ref, rec.Owner, "request", request, rec.Refs)
		})
		return saveErr
	}
	p := access.Principal{UserID: request.Actor.User}
	sources := []Ref{request.Draft, request.Accepted}
	for _, ref := range rec.Refs {
		if fixed(ref) {
			sources = append(sources, ref)
		}
	}
	spec := runs.Spec{Name: request.Input.Name, Input: runs.Input{Command: runs.Command{Kind: "server-connector", Target: request.Input.Connection, Parameters: raw(map[string]any{"repository_connection": request.Input.Connection, "title": request.Input.Title, "body": request.Body})}}, Sources: unique(sources), Derivation: &runs.Contract{Name: "ExternalIssueCreation", Version: "github-issues-v1"}}
	created, err := accepted(s.Runs.CreateForConnector(ctx, p, w, request.Run, runs.Create{Mutation: apicontract.Mutation{OperationID: request.Run + ":create", ExpectedVersion: "0"}, Spec: spec}))
	if err != nil {
		return err
	}
	sending, claimed, err := s.change(ctx, w, rec, "send", func(tx pgx.Tx, ref Ref) (Record, error) {
		if err := s.authority(ctx, tx, w, request, rec.Owner, true); err != nil {
			return Record{}, err
		}
		request.State = "sending"
		request.Reason = ""
		if _, err := tx.Exec(ctx, `UPDATE publication_queue SET state='sending',due_at=now()+interval '2 minutes' WHERE workspace_id=$1 AND id=$2 AND state='accepted'`, w, id); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, ref, rec.Owner, "request", request, rec.Refs)
	})
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if _, err = accepted(s.Runs.RecordConnector(ctx, w, request.Run, request.Accepted, apicontract.Mutation{OperationID: request.Run + ":start", ExpectedVersion: created.Version}, true, nil, 0)); err != nil {
		return err
	}
	// The final authorization check is adjacent to I/O, outside a held DB tx.
	if err = s.authority(ctx, s.Store.Database(), w, request, rec.Owner, true); err != nil {
		return err
	}
	connection, ok := s.Backend.Connection(w, request.Input.Connection)
	if !ok {
		return access.ErrForbidden
	}
	result := s.Backend.Create(ctx, connection, request.Input, request.Body)
	observed, err := s.observe(ctx, w, sending, result, true)
	if err != nil {
		return err
	}
	return s.finishRun(ctx, w, observed)
}
func (s *Service) refresh(ctx context.Context, w string, rec Record) (Record, error) {
	var request Request
	if decode(rec.Value, &request) != nil {
		return Record{}, ErrInvalid
	}
	if request.State != "created" && request.State != "unknown" {
		return rec, nil
	}
	if s.Backend == nil {
		return Record{}, ErrState
	}
	c, ok := s.Backend.Connection(w, request.Input.Connection)
	if !ok {
		return Record{}, access.ErrForbidden
	}
	number := int64(0)
	if request.External != nil {
		number = request.External.Number
	}
	result := s.Backend.Observe(ctx, c, request.Marker, number)
	// An unavailable current location does not undo historical creation.
	if request.State == "created" && result.State == "unknown" {
		result.State = "created"
	}
	return s.observe(ctx, w, rec, result, false)
}
func (s *Service) Refresh(ctx context.Context, p access.Principal, w string, ref Ref) (Record, error) {
	rec, err := s.Read(ctx, p, w, ref)
	if err != nil {
		return Record{}, err
	}
	if rec.Kind != "request" {
		return Record{}, ErrInvalid
	}
	latest, err := record(ctx, s.Store.Database(), w, Ref{ID: rec.ID, Version: "head"})
	if err != nil {
		return Record{}, err
	}
	if latest.Ref != rec.Ref {
		return Record{}, ErrState
	}
	updated, err := s.refresh(ctx, w, rec)
	if err != nil {
		return Record{}, err
	}
	return s.Read(ctx, p, w, updated.Ref)
}
func (s *Service) StartWorker(ctx context.Context) {
	go func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				rows, err := s.Store.Database().Query(ctx, `SELECT workspace_id,id FROM publication_queue WHERE state IN ('accepted','sending','unknown','finishing') AND due_at<=now() ORDER BY due_at,workspace_id,id LIMIT 25`)
				if err != nil {
					continue
				}
				type key struct{ w, id string }
				keys := []key{}
				for rows.Next() {
					var k key
					if rows.Scan(&k.w, &k.id) == nil {
						keys = append(keys, k)
					}
				}
				rows.Close()
				for _, k := range keys {
					_ = s.Process(ctx, k.w, k.id)
				}
			}
		}
	}()
}
