package automation

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/jackc/pgx/v5"
)

// The publication owner calls this only for an observed Issue. Receiving this
// link grants no permission to publish labels or to execute a Run.
func (s *Service) BindPublicationTx(ctx context.Context, tx pgx.Tx, w string, source Ref, connection string, number int64, refs []Ref) error {
	ids := []string{}
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	rows, err := tx.Query(ctx, `SELECT scope_id FROM triage_targets WHERE workspace_id=$1 AND scope_id=ANY($2) ORDER BY scope_id LIMIT 2`, w, ids)
	if err != nil {
		return err
	}
	targets := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(targets) != 1 {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO triage_bindings(workspace_id,scope_id,connection,number,source_id,source_version) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, w, targets[0], connection, number, source.ID, source.Version)
	return err
}

func (s *Service) BindPublication(ctx context.Context) (bool, error) {
	var w, scopeID, connection string
	var number int64
	var source Ref
	err := s.Store.Database().QueryRow(ctx, `UPDATE triage_bindings SET due_at=now()+make_interval(secs=>$1) WHERE (workspace_id,scope_id)=(SELECT workspace_id,scope_id FROM triage_bindings WHERE due_at<=now() ORDER BY due_at,workspace_id,scope_id LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING workspace_id,scope_id,connection,number,source_id,source_version`, s.priorityInterval()).Scan(&w, &scopeID, &connection, &number, &source.ID, &source.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	var id, version string
	if err = s.Store.Database().QueryRow(ctx, `SELECT info_id,info_version FROM triage_targets WHERE workspace_id=$1 AND scope_id=$2`, w, scopeID).Scan(&id, &version); err != nil {
		return true, err
	}
	rec, err := record(ctx, s.Store.Database(), w, Ref{ID: id, Version: version})
	if err != nil {
		return true, err
	}
	var info Issue
	if json.Unmarshal(rec.Value, &info) != nil {
		return true, ErrInvalid
	}
	if info.External != nil {
		_, err = s.Store.Database().Exec(ctx, `DELETE FROM triage_bindings WHERE workspace_id=$1 AND scope_id=$2`, w, scopeID)
		return true, err
	}
	selected, reason, _, err := s.selectPolicy(ctx, s.Store.Database(), w, info.Scope)
	if err != nil || reason != "" {
		return true, err
	}
	var policy Policy
	_ = json.Unmarshal(selected.Value, &policy)
	if policy.External == nil || policy.External.Connection != connection {
		return true, nil
	}
	info.External = &ExternalPriority{Connection: connection, Number: number}
	info.Sources = append(info.Sources, source)
	_, err = s.systemTriage(ctx, w, id, "observed-issue", "bind:"+source.ID+"@"+source.Version, version, func(tx pgx.Tx) error {
		c, err := control(ctx, tx, w, "triage")
		if err != nil || !c.Enabled || !c.CanCommit {
			return access.ErrForbidden
		}
		p := access.Principal{UserID: c.Controller}
		if err = s.Access.CheckTx(ctx, tx, p, w, "", "manage"); err != nil {
			return err
		}
		if err = s.Access.CheckTx(ctx, tx, p, w, scopeID, "execute"); err != nil {
			return err
		}
		return s.checkRefs(ctx, tx, p, w, append(rec.Refs, source))
	}, func(tx pgx.Tx, ref Ref) (Record, error) {
		refs := append(append([]Ref{}, rec.Refs...), source)
		_, err := s.save(ctx, tx, w, ref, "issue", info, refs)
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM triage_bindings WHERE workspace_id=$1 AND scope_id=$2`, w, scopeID)
		}
		return Record{Ref: ref, Kind: "issue", Value: encode(info), Refs: refs}, err
	})
	return true, err
}
