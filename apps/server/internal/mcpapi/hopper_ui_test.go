package mcpapi

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/hopper"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
	"time"
)

func prepareHopperUI(t *testing.T, ctx context.Context, s *Service, p access.Principal) func(http.ResponseWriter, *http.Request) bool {
	t.Helper()
	store := s.Access.Store
	serial := 0
	mutation := func(version string) apicontract.Mutation {
		serial++
		return apicontract.Mutation{OperationID: "hopper-ui-" + time.Now().Format("150405.000000000"), ExpectedVersion: version}
	}
	must := func(v apicontract.Response[apicontract.Reference], err error) access.VersionRef {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatal(v, err)
		}
		return access.VersionRef{ID: v.Data.ID, Version: v.Data.Version}
	}
	must(s.Automation.Put(ctx, p, "mcp-one", "control:hopper", automation.Write{Mutation: mutation("0"), Value: encodeTest(automation.Control{Kind: "hopper", Enabled: true, CanRead: true, CanCommit: true, Slots: 2, MaxAttempts: 3, LeaseSeconds: 60, TimeoutSeconds: 30, RetrySeconds: 1, MaxRequests: 1000, PeriodSeconds: 3600})}))
	source := must(s.Runs.Create(ctx, p, "mcp-one", "run:hopper-ui-input", runs.Create{Mutation: mutation("0"), Spec: runs.Spec{Name: "受入: Hopper判定用の合成結果", Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "fixture"}}}}))
	template, err := s.Workflows.Read(ctx, p, "mcp-one", access.VersionRef{ID: "run-definition:hopper-mcp", Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	definition := hopper.Template()
	definition.Name = "受入: 結果から次の仕事を発行"
	definition.Problem = nil
	definition.Targets = []hopper.Target{{Kind: "run", ID: source.ID}}
	definition.Condition = hopper.Condition{Field: "run.state", Op: "eq", Value: json.RawMessage(`"failed"`)}
	definition.Action.Template = template.Ref
	must(s.Hopper.Put(ctx, p, "mcp-one", "hopper:ui-next-work", hopper.Write{Mutation: mutation("0"), Value: encodeTest(definition)}))
	s.Hopper.Start(ctx)
	// This route exists only on the owned test gateway. It accepts no result data
	// from the browser and emits one fixed synthetic acceptance, never a real CLI.
	doneAt := time.Now().UTC()
	event := apicontract.Mutation{OperationID: "hopper-ui-fixed-result", ExpectedVersion: source.Version}
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/__fixture/hopper-result" || r.Method != "POST" {
			return false
		}
		v, err := store.Apply(ctx, persistence.Scope{ActorID: "fixture:owned-result", Grant: apicontract.Grant{WorkspaceID: "mcp-one", CanRead: true, CanUpdate: true}}, persistence.Command{WorkspaceID: "mcp-one", ResourceID: source.ID, Kind: "run-report", Mutation: event, Payload: json.RawMessage(`{"type":"finished","fixture":"hopper"}`)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
			var b []byte
			var dependencies []byte
			if err := tx.QueryRow(ctx, `SELECT payload,refs FROM run_records WHERE workspace_id='mcp-one' AND id=$1 AND version=$2`, source.ID, source.Version).Scan(&b, &dependencies); err != nil {
				return nil, err
			}
			var run runs.Run
			var refs []access.VersionRef
			_ = json.Unmarshal(b, &run)
			_ = json.Unmarshal(dependencies, &refs)
			run.State = "failed"
			run.FinishedAt = &doneAt
			if _, err := tx.Exec(ctx, `INSERT INTO run_records SELECT $1,$2,$2,$3,'run',sequence+1,$4,$5 FROM shared_streams WHERE workspace_id=$1`, "mcp-one", source.ID, ref.Version, encodeTest(run), dependencies); err != nil {
				return nil, err
			}
			return encodeTest(runs.Record{Ref: access.VersionRef{ID: source.ID, Version: ref.Version}, RunID: source.ID, Kind: "run", Value: encodeTest(run), Refs: refs}), nil
		})
		if err != nil || v.Data == nil {
			http.Error(w, "owned result acceptance failed", 500)
		} else {
			w.WriteHeader(204)
		}
		return true
	}
}
