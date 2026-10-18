package hopper

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// The index selects only active logical evaluations watching this exact
// evidence identity; Context and definition edits do not scan old evaluations.
func (s *Service) evidenceChanged(ctx context.Context, tx pgx.Tx, w string, changed Ref) error {
	rows, err := tx.Query(ctx, `SELECT q.evaluation_id,q.definition_id,q.definition_version,s.payload FROM hopper_evidence_watches q JOIN hopper_sources s ON s.position=q.source_position LEFT JOIN shared_versions h ON h.workspace_id=q.workspace_id AND h.resource_id='hopper-active:'||substring(q.definition_id from 8) LEFT JOIN hopper_records a ON a.workspace_id=h.workspace_id AND a.id=h.resource_id AND a.version=h.version WHERE q.workspace_id=$1 AND q.resource_id=$2 AND (q.dry_run OR (a.payload->>'enabled'='true' AND a.payload->'definition'->>'version'=q.definition_version))`, w, changed.ID)
	if err != nil {
		return err
	}
	type watch struct {
		Evaluation string
		Definition Ref
		Source     Source
	}
	watches := []watch{}
	for rows.Next() {
		var value watch
		var b []byte
		if err = rows.Scan(&value.Evaluation, &value.Definition.ID, &value.Definition.Version, &b); err != nil {
			break
		}
		if err = json.Unmarshal(b, &value.Source); err != nil {
			break
		}
		watches = append(watches, value)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, watch := range watches {
		source := watch.Source
		if previous, err := record(ctx, tx, w, Ref{ID: watch.Evaluation, Version: "head"}); err == nil {
			source.Previous = &previous.Ref
		} else if !missing(err) {
			return err
		}
		source.Event = "evidence:" + hash([]any{source.Event, changed, watch.Definition})
		if err = s.AcceptSourceTx(ctx, tx, w, source, watch.Definition); err != nil {
			return err
		}
	}
	return nil
}
