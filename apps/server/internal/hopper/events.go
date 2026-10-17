package hopper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
)

func (s *Service) inherited(ctx context.Context, tx pgx.Tx, w, id string, refs []Ref) error {
	ids := []string{}
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	_, err := tx.Exec(ctx, `INSERT INTO hopper_lineage SELECT $1,$2,l.root,l.depth,l.evaluation_id,l.evaluation_version FROM hopper_lineage l WHERE l.workspace_id=$1 AND l.resource_id=ANY($3) ORDER BY l.depth DESC,l.resource_id LIMIT 1 ON CONFLICT DO NOTHING`, w, id, ids)
	return err
}
func (s *Service) accepted(ctx context.Context, tx pgx.Tx, c persistence.Command, ref apicontract.Reference, state json.RawMessage) error {
	if strings.HasPrefix(c.Kind, "hopper-") || c.Deleted {
		return nil
	}
	var envelope struct {
		Value json.RawMessage `json:"value"`
		Refs  []Ref           `json:"refs"`
	}
	if json.Unmarshal(state, &envelope) != nil {
		return nil
	}
	if err := s.inherited(ctx, tx, c.WorkspaceID, ref.ID, envelope.Refs); err != nil {
		return err
	}
	// Tasks inherit their execution's root before their normal Run is prepared.
	if strings.HasPrefix(c.Kind, "workflow-") {
		if _, err := tx.Exec(ctx, `INSERT INTO hopper_lineage SELECT r.workspace_id,r.id,l.root,l.depth,l.evaluation_id,l.evaluation_version FROM workflow_records r JOIN hopper_lineage l ON l.workspace_id=r.workspace_id AND l.resource_id=r.owner_id WHERE r.workspace_id=$1 AND r.version=$2 AND r.kind='task' ON CONFLICT DO NOTHING`, c.WorkspaceID, ref.Version); err != nil {
			return err
		}
	}
	if !strings.HasPrefix(ref.ID, "run:") || len(envelope.Value) == 0 {
		return nil
	}
	var run runs.Run
	if json.Unmarshal(envelope.Value, &run) != nil {
		return nil
	}
	var inputRefs []byte
	if err := tx.QueryRow(ctx, `SELECT refs FROM run_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='input'`, c.WorkspaceID, run.Input.ID, run.Input.Version).Scan(&inputRefs); err != nil {
		return err
	}
	var dependencies []Ref
	if err := json.Unmarshal(inputRefs, &dependencies); err != nil {
		return err
	}
	envelope.Refs = append(envelope.Refs, dependencies...)
	if err := s.inherited(ctx, tx, c.WorkspaceID, ref.ID, envelope.Refs); err != nil {
		return err
	}
	if run.State != "succeeded" && run.State != "failed" && run.State != "cancelled" {
		return nil
	}
	// A renewal after terminal output is not a new result event.
	if !strings.HasPrefix(c.Kind, "run-") {
		return nil
	}
	var payload struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(c.Payload, &payload)
	if c.Kind == "run-report" && payload.Type != "finished" {
		return nil
	}
	f := Facts{State: run.State, ExitCode: run.ExitCode}
	if run.Assessment != nil {
		f.Verdict = run.Assessment.Verdict
		f.Report = run.Assessment.Report
	}
	keys := []string{ref.ID, "*"}
	for _, r := range envelope.Refs {
		if strings.HasPrefix(r.ID, "scope:") {
			keys = append(keys, r.ID)
		}
	}
	return s.AcceptSourceTx(ctx, tx, c.WorkspaceID, Source{Event: ref.ID + "@" + ref.Version, Target: Ref{ID: ref.ID, Version: ref.Version}, Kind: "run", Keys: keys, Facts: f, Refs: envelope.Refs})
}

// AcceptSourceTx is called only by trusted result owners in their acceptance tx.
// No HTTP/MCP route can submit fabricated result observations.
func (s *Service) AcceptSourceTx(ctx context.Context, tx pgx.Tx, w string, source Source) error {
	if source.Event == "" || !fixed(source.Target) || (source.Kind != "run" && source.Kind != "github") {
		return ErrInvalid
	}
	err := tx.QueryRow(ctx, `SELECT root,depth FROM hopper_lineage WHERE workspace_id=$1 AND resource_id=$2`, w, source.Target.ID).Scan(&source.Root, &source.Depth)
	if err != nil && !missing(err) {
		return err
	}
	if source.Root == "" {
		source.Root = hash([]string{w, source.Target.ID})
	}
	if source.Refs == nil {
		source.Refs = []Ref{}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO hopper_sources(workspace_id,event_id,kind,target_id,target_version,payload) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, w, source.Event, source.Kind, source.Target.ID, source.Target.Version, raw(source)); err != nil {
		return err
	}
	var position int64
	if err = tx.QueryRow(ctx, `SELECT position FROM hopper_sources WHERE workspace_id=$1 AND event_id=$2 AND target_id=$3 AND target_version=$4`, w, source.Event, source.Target.ID, source.Target.Version).Scan(&position); err != nil {
		return err
	}
	for _, key := range source.Keys {
		if _, err = tx.Exec(ctx, `INSERT INTO hopper_source_keys VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, w, source.Kind, key, position); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.payload FROM hopper_targets t JOIN shared_versions h ON h.workspace_id=t.workspace_id AND h.resource_id='hopper-active:'||substring(t.definition_id from 8) JOIN hopper_records a ON a.workspace_id=h.workspace_id AND a.id=h.resource_id AND a.version=h.version WHERE t.workspace_id=$1 AND t.kind=$2 AND t.target=ANY($3)`, w, source.Kind, source.Keys)
	if err != nil {
		return err
	}
	activations := []Activation{}
	for rows.Next() {
		var b []byte
		var a Activation
		if err = rows.Scan(&b); err != nil {
			break
		}
		err = json.Unmarshal(b, &a)
		if err != nil {
			break
		}
		activations = append(activations, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range activations {
		r, err := record(ctx, tx, w, a.Definition)
		if err != nil {
			return err
		}
		var d Definition
		_ = json.Unmarshal(r.Value, &d)
		if !a.Enabled || source.Depth > 0 && !d.IncludeDerived {
			continue
		}
		if err = s.Automation.AcceptResultTx(ctx, tx, w, automation.Source{Event: source.Event, Target: source.Target}, a.Definition); err != nil {
			return err
		}
	}
	return nil
}

// GitHubAccepted attaches to the observer's committed version change, including
// periodic reconciliation without any running client or LLM gateway.
func (s *Service) GitHubAccepted(ctx context.Context, tx pgx.Tx, target githubobserve.Target, snapshot githubobserve.Snapshot, version int64) error {
	if target.Kind != "actions" || snapshot.State != "completed" {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id FROM github_bindings WHERE target_id=$1`, target.ID)
	if err != nil {
		return err
	}
	workspaces := []string{}
	for rows.Next() {
		var w string
		if err = rows.Scan(&w); err != nil {
			break
		}
		workspaces = append(workspaces, w)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, w := range workspaces {
		id := "hopper-result:" + target.ID
		event := fmt.Sprintf("github:%s:%d", target.ID, version)
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hopper_sources WHERE workspace_id=$1 AND event_id=$2)`, w, event).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		var expected string
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2),'0')`, w, id).Scan(&expected); err != nil {
			return err
		}
		source := Source{Event: event, Kind: "github", Keys: []string{target.ID, "*"}, Facts: Facts{Conclusion: snapshot.Conclusion, Report: raw(snapshot)}, Refs: []Ref{{ID: "github:" + target.ID}}, Root: hash([]any{w, target.ID, snapshot.RunID, snapshot.Attempt})}
		// This internal resource is readable only through currently configured binding
		// and definitions. Shared scope/resource grants still apply to all its refs.
		v, err := s.Store.Apply(ctx, persistence.Scope{ActorID: "observer:github", Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "hopper-source", Mutation: apicontract.Mutation{OperationID: source.Event, ExpectedVersion: expected}, Payload: raw(source)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
			source.Target = Ref{ID: id, Version: ref.Version}
			r, err := s.save(ctx, tx, w, source.Target, "source", source, source.Refs)
			if err != nil {
				return nil, err
			}
			return raw(r), s.AcceptSourceTx(ctx, tx, w, source)
		}, tx)
		if _, err = accepted(v, err); err != nil {
			return err
		}
	}
	return nil
}
