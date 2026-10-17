package mcpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/inspections"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
	"strings"
	"testing"
	"time"
)

func prepareInspectionUI(t *testing.T, s *inspections.Service, p access.Principal, key []byte) {
	t.Helper()
	raw := func(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
	ctx := context.Background()
	w := "mcp-one"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := s.Access.DB.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE access_members SET can_execute=true WHERE workspace_id=$1 AND user_id=$2`, w, p.UserID)
	device := access.Principal{UserID: p.UserID, DeviceID: "inspection-ui-device"}
	exec(`INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES($1,$2,$3,$1,'独立検品の固定接続','g',true,true)`, device.DeviceID, w, p.UserID)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("device\x00" + device.DeviceID + "\x00g"))
	var err error
	device, err = s.Access.DevicePrincipal(ctx, w, "d."+device.DeviceID+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	mutation := func(version string) apicontract.Mutation {
		n++
		return apicontract.Mutation{OperationID: strings.Repeat("i", n), ExpectedVersion: version}
	}
	accept := func(v apicontract.Response[apicontract.Reference], err error) inspections.Ref {
		t.Helper()
		if err != nil || v.Data == nil {
			t.Fatalf("inspection UI fixture step %d: %+v %v", n, v, err)
		}
		return inspections.Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	accept(s.Runs.ContactRunner(ctx, device, w, runs.RunnerContact{Mutation: mutation("0"), RunnerID: "inspection-ui-runner", Connections: []runs.Connection{{Kind: "cli", Target: "inspection-fixture", Version: "fixed-v1", Environment: "fixture", Subject: "fixture", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"inspection/session"}, Users: []string{p.UserID}}}}))
	template := workflows.Definition{Schema: "run-definition-v1", Name: "検品用の固定command", Spec: &runs.Spec{Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "inspection-fixture", Binding: map[string]string{"prompt": "stdin"}}}, Derivation: &runs.Contract{Name: "InspectionRun", Version: "v1", VerdictField: "verdict"}}}
	tr := accept(s.Workflows.Put(ctx, p, w, "run-definition:ui-inspector", workflows.Write{Mutation: mutation("0"), Value: raw(template)}))
	target := contexts.Fixed{URI: "fixture://inspection-artifact", Revision: "fixed-v1", Range: "whole", Digest: strings.Repeat("a", 64), Delivery: "reference", State: "ready"}
	evidence := inspections.Evidence{Artifact: target, Targets: []contexts.Fixed{target}, Criteria: []string{"AC-1"}, Kind: "machine-observation", Description: "固定CLIの検品ログ", State: "available", CheckedAt: time.Now().UTC()}
	er := accept(s.Put(ctx, p, w, "inspection-evidence:ui-log", inspections.Write{Mutation: mutation("0"), Value: raw(evidence)}))
	definition := inspections.Definition{Name: "受入: 固定成果物の独立検品", Kind: "InspectionRun", Purpose: "fresh", Template: tr, Targets: []contexts.Fixed{target}, Criteria: []inspections.Criterion{{ID: "AC-1", Requirement: "固定成果物と明示した条件を検品する"}}, Selection: contexts.Selection{Mode: "none"}, Required: map[string]string{}}
	dr := accept(s.Put(ctx, p, w, "inspection:ui-fixed", inspections.Write{Mutation: mutation("0"), Value: raw(definition)}))
	adoption := accept(s.Adopt(ctx, p, w, inspections.Adopt{Mutation: mutation("0"), Definition: dr}))
	for _, name := range []string{"pass", "fail"} {
		run := "run:ui-inspection-" + name
		lr := accept(s.Start(ctx, p, w, run, inspections.Start{Mutation: mutation("0"), Adoption: adoption, Device: device.DeviceID}))
		rec, err := s.Read(ctx, p, w, lr)
		if err != nil {
			t.Fatal(err)
		}
		var l inspections.Launch
		_ = json.Unmarshal(rec.Value, &l)
		head, err := s.Runs.Read(ctx, p, w, runs.Ref{ID: run, Version: "head"})
		if err != nil {
			t.Fatal(err)
		}
		var state runs.Run
		_ = json.Unmarshal(head.Value, &state)
		ref := head.Ref
		event := func(sequence string, e runs.Event) {
			e.Mutation = mutation(ref.Version)
			e.Generation = state.Assignment.Generation
			e.Sequence = sequence
			ref = accept(s.Runs.Report(ctx, device, w, run, e))
		}
		event("1", runs.Event{Type: "prepared"})
		event("2", runs.Event{Type: "started"})
		verdict := "PASS"
		if name == "fail" {
			verdict = "FAIL"
		}
		report := inspections.Report{Inspection: dr, ManifestDigest: l.Manifest.Digest, Verdict: verdict, Results: []inspections.CriterionResult{{ID: "AC-1", Verdict: verdict, Executed: true, Evidence: []inspections.Ref{er}, Unverified: []string{}}}, Unverified: []string{}}
		zero := 0
		event("3", runs.Event{Type: "finished", ExitCode: &zero, Report: raw(report)})
	}
	evidence.State = "expired"
	accept(s.Put(ctx, p, w, er.ID, inspections.Write{Mutation: mutation(er.Version), Value: raw(evidence)}))
	definition.Criteria[0].Requirement = "更新後の固定条件"
	next := accept(s.Put(ctx, p, w, dr.ID, inspections.Write{Mutation: mutation(dr.Version), Value: raw(definition)}))
	accept(s.Adopt(ctx, p, w, inspections.Adopt{Mutation: mutation(adoption.Version), Definition: next}))
}
