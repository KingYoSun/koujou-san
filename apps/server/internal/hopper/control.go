package hopper

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type Decision struct {
	apicontract.Mutation
	Action string  `json:"action"`
	Bounds *Bounds `json:"bounds,omitempty"`
}

func (s *Service) Decide(ctx context.Context, p access.Principal, w, id string, req Decision) (apicontract.Response[apicontract.Reference], error) {
	if req.Action != "publish" && req.Action != "stop" && req.Action != "resume" && req.Action != "reevaluate" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutation(ctx, p, w, id, "manage", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		if strings.HasPrefix(id, "hopper-chain:") {
			if req.Action == "publish" || req.Action == "reevaluate" {
				return Record{}, ErrInvalid
			}
			root := strings.TrimPrefix(id, "hopper-chain:")
			// Require a currently readable evaluation before returning any chain state.
			var b []byte
			var refs []byte
			if err := tx.QueryRow(ctx, `SELECT r.refs FROM hopper_records r WHERE workspace_id=$1 AND kind='evaluation' AND payload->>'root'=$4 AND `+access.ReadFilterSQL("r.refs")+` ORDER BY position DESC LIMIT 1`, w, p.UserID, p.OperationID, root).Scan(&refs); err != nil {
				return Record{}, access.ErrForbidden
			}
			var dependencies []Ref
			_ = json.Unmarshal(refs, &dependencies)
			if req.Bounds != nil {
				v := *req.Bounds
				if v.Depth < 1 || v.Publications < 1 || v.Evaluations < 1 || v.Seconds < 1 || v.PerEvaluation < 1 {
					return Record{}, ErrInvalid
				}
				if _, err := tx.Exec(ctx, `UPDATE hopper_chains SET bounds=$3 WHERE workspace_id=$1 AND id=$2`, w, root, raw(v)); err != nil {
					return Record{}, err
				}
			}
			if err := tx.QueryRow(ctx, `UPDATE hopper_chains SET stopped=$3 WHERE workspace_id=$1 AND id=$2 RETURNING bounds`, w, root, req.Action == "stop").Scan(&b); err != nil {
				return Record{}, err
			}
			return s.save(ctx, tx, w, ref, "chain", struct {
				Root    string          `json:"root"`
				Stopped bool            `json:"stopped"`
				Bounds  json.RawMessage `json:"bounds"`
			}{root, req.Action == "stop", b}, dependencies)
		}
		if req.Action != "publish" && req.Action != "resume" && req.Action != "reevaluate" {
			return Record{}, ErrInvalid
		}
		r, err := record(ctx, tx, w, Ref{ID: id, Version: req.ExpectedVersion})
		if err != nil || r.Kind != "evaluation" {
			return Record{}, ErrInvalid
		}
		if err = s.refs(ctx, tx, p, w, append(r.Refs, r.Ref)); err != nil {
			return Record{}, err
		}
		var e Evaluation
		if json.Unmarshal(r.Value, &e) != nil || (e.DryRun && req.Action != "reevaluate") || (req.Action == "resume" && (e.State != "published" || e.Destination == nil || !strings.HasPrefix(e.Destination.ID, "run:"))) || (req.Action == "publish" && e.State != "proposal" && e.State != "held" && e.State != "blocked" && e.State != "input_waiting") {
			return Record{}, ErrState
		}
		controller, control, err := s.Automation.CheckManualTx(ctx, tx, w, r.Refs)
		if err != nil {
			return Record{}, access.ErrForbidden
		}
		if !e.DryRun {
			a, err := s.active(ctx, tx, w, e.Definition)
			if err != nil || controller.Controller != a.Controller {
				return Record{}, access.ErrForbidden
			}
		}
		actor := access.Principal{UserID: controller.Controller}
		if err = s.Access.CheckTx(ctx, tx, actor, w, "", "manage"); err != nil {
			return Record{}, err
		}
		dr, err := record(ctx, tx, w, e.Definition)
		if err != nil {
			return Record{}, err
		}
		var d Definition
		_ = json.Unmarshal(dr.Value, &d)
		if req.Action == "reevaluate" {
			if !hasSemantic(d.Condition) {
				return Record{}, ErrInvalid
			}
			source := e.Source
			source.Previous, source.RequestedBy = &r.Ref, p.UserID
			source.Event = "reevaluate:" + hash([]any{r.Ref, req.OperationID})
			if err = s.AcceptSourceTx(ctx, tx, w, source, e.Definition); err != nil {
				return Record{}, err
			}
			e.Reevaluation = "hopper-evaluation:" + hash([]string{w, source.Event, source.Target.ID, source.Target.Version, e.Definition.ID, e.Definition.Version})
			return s.save(ctx, tx, w, ref, "evaluation", e, r.Refs)
		}
		e.At = time.Now().UTC()
		if req.Action == "resume" {
			tag, err := tx.Exec(ctx, `UPDATE hopper_dispatch q SET state='waiting',reason='',due_at=now() FROM run_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version WHERE q.workspace_id=$1 AND q.run_id=$2 AND q.state IN ('waiting','held') AND r.workspace_id=q.workspace_id AND r.id=q.run_id AND r.payload->>'state'='queued' AND r.payload->'assignment' IS NULL AND COALESCE((r.payload->>'cancel_requested')::boolean,false)=false`, w, e.Destination.ID)
			if err != nil {
				return Record{}, err
			}
			if tag.RowsAffected() != 1 {
				return Record{}, ErrState
			}
			e.Reason = "assignment_resumed"
			return s.save(ctx, tx, w, ref, "evaluation", e, r.Refs)
		}
		e.Reason = ""
		e.State = "evaluated"
		if err = s.recheckSemantic(ctx, tx, actor, w, &e, d); err != nil {
			return Record{}, err
		}
		if err = s.evaluate(ctx, tx, w, actor, &e, d, ref, control, true); err != nil {
			return Record{}, err
		}
		if e.Cost != nil {
			r.Refs = append(r.Refs, e.Cost.Sources...)
		}
		r.Refs = append(append(r.Refs, e.Source.Refs...), e.audit...)
		return s.save(ctx, tx, w, ref, "evaluation", e, r.Refs)
	})
}
func (s *Service) Past(ctx context.Context, p access.Principal, w string, req Past) (apicontract.Response[apicontract.Reference], error) {
	if !fixed(req.Definition) || req.ExpectedVersion != "0" || req.Target == "" || (req.Kind != "run" && req.Kind != "github") || req.Limit < 1 || req.Limit > 100 || req.After < 0 {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutation(ctx, p, w, "hopper-past:"+req.OperationID, "manage", req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		r, err := record(ctx, tx, w, req.Definition)
		if err != nil || r.Kind != "definition" {
			return Record{}, ErrInvalid
		}
		var d Definition
		_ = json.Unmarshal(r.Value, &d)
		if err = s.refs(ctx, tx, p, w, append(r.Refs, r.Ref)); err != nil {
			return Record{}, err
		}
		rows, err := tx.Query(ctx, `SELECT s.position,s.payload FROM hopper_source_keys k JOIN hopper_sources s ON s.position=k.position WHERE k.workspace_id=$1 AND k.kind=$4 AND k.key=$5 AND k.position>$6 AND `+access.ReadFilterSQL(`COALESCE(s.payload->'refs','[]')||jsonb_build_array(s.payload->'target')`)+` ORDER BY k.position LIMIT $7`, w, p.UserID, p.OperationID, req.Kind, req.Target, req.After, req.Limit)
		if err != nil {
			return Record{}, err
		}
		sources := []Source{}
		after := req.After
		for rows.Next() {
			var source Source
			var b []byte
			if err = rows.Scan(&after, &b); err != nil {
				break
			}
			if err = json.Unmarshal(b, &source); err != nil {
				break
			}
			sources = append(sources, source)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return Record{}, err
		}
		items := []Ref{}
		dependencies := append(append([]Ref{}, r.Refs...), r.Ref)
		for _, source := range sources {
			if hasSemantic(d.Condition) || req.Publish {
				source.Event = "past:" + hash([]any{req.OperationID, source.Event, r.Ref})
				source.DryRun = !req.Publish
				source.RequestedBy = p.UserID
				if err = s.AcceptSourceTx(ctx, tx, w, source, r.Ref); err != nil {
					return Record{}, err
				}
				items = append(items, Ref{ID: "hopper-evaluation:" + hash([]string{w, source.Event, source.Target.ID, source.Target.Version, r.ID, r.Version})})
				dependencies = append(dependencies, source.Refs...)
				dependencies = append(dependencies, source.Target)
				continue
			}
			e := Evaluation{Definition: r.Ref, Source: source, Root: source.Root, Depth: source.Depth, DryRun: true, State: "evaluated", At: time.Now().UTC()}
			trial := Ref{ID: "hopper-trial:" + hash([]string{ref.ID, source.Event, source.Target.Version}), Version: ref.Version}
			if err = s.evaluate(ctx, tx, w, p, &e, d, trial, Ref{}, false); err != nil {
				return Record{}, err
			}
			refs := append(append([]Ref{}, dependencies...), append(source.Refs, source.Target)...)
			if _, err = s.save(ctx, tx, w, trial, "evaluation", e, refs); err != nil {
				return Record{}, err
			}
			items = append(items, trial)
			dependencies = append(dependencies, source.Refs...)
			dependencies = append(dependencies, source.Target)
		}
		return s.save(ctx, tx, w, ref, "past", struct {
			Items []Ref `json:"items"`
			After int64 `json:"after"`
		}{items, after}, dependencies)
	})
}
