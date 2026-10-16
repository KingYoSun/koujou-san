package mcpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
)

// A disposable Workspace keeps assignment tests separate from the Context and
// Pause fixtures. The registered device and all policy changes are fixture-owned.
func prepareRunnerUI(t *testing.T, client *http.Client, target string, tokens auth.Tokens, s *Service, user string) func() {
	t.Helper()
	ctx := context.Background()
	workspace := "runner-ui"
	post := func(path string, value any) []byte {
		t.Helper()
		b, _ := json.Marshal(value)
		req, _ := http.NewRequest("POST", target+path, strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+tokens.Access)
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("owned Runner fixture %s: %d", path, res.StatusCode)
		}
		return out
	}
	post("/access/workspaces/runner-ui/create", map[string]any{"name": "Runner受入", "operation_id": "runner-ui-create", "expected_version": "0"})
	if _, e := s.Access.DB.Exec(ctx, `UPDATE access_members SET can_execute=true WHERE workspace_id=$1 AND user_id=$2`, workspace, user); e != nil {
		t.Fatal(e)
	}
	enrolled := post("/access/workspaces/runner-ui/device-register", map[string]any{"id": "runner-web-fixture", "identifier": "runner-web-fixture", "name": "Web割当の検証端末", "operation_id": "runner-web-enroll", "expected_version": "0"})
	var private struct {
		Credential string `json:"credential"`
	}
	_ = json.Unmarshal(enrolled, &private)
	if _, e := s.Access.DB.Exec(ctx, `UPDATE access_devices SET can_input=true,can_report=true WHERE workspace_id=$1 AND id='runner-web-fixture'`, workspace); e != nil {
		t.Fatal(e)
	}
	p, e := s.Access.DevicePrincipal(ctx, workspace, private.Credential)
	if e != nil {
		t.Fatal(e)
	}
	refresh := func() {
		old := "0"
		var payload []byte
		if e := s.Access.DB.QueryRow(ctx, `SELECT payload FROM run_runners WHERE workspace_id=$1 AND device_id=$2`, workspace, p.DeviceID).Scan(&payload); e == nil {
			var v runs.Runner
			_ = json.Unmarshal(payload, &v)
			old = v.Version
		}
		_, e := s.Runs.ContactRunner(ctx, p, workspace, runs.RunnerContact{Mutation: apicontract.Mutation{OperationID: rand.Text(), ExpectedVersion: old}, RunnerID: "web-fixture-runner", Connections: []runs.Connection{{Kind: "cli", Target: "git", Version: "fixture-git-v1", Environment: "fixed-fixture", Subject: "local-git-fixture", Confirmed: true, Users: []string{user}, Repositories: []string{"fixture-repository"}}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	refresh()
	for _, id := range []string{"ui-assign-native", "ui-assign-native-two", "ui-assign-web"} {
		out, e := s.Runs.Create(ctx, access.Principal{UserID: user}, workspace, id, runs.Create{Mutation: apicontract.Mutation{OperationID: "runner-create-" + id, ExpectedVersion: "0"}, Spec: runs.Spec{Name: "割当受入: " + id, Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "git", Parameters: json.RawMessage(`["status"]`)}}}})
		if e != nil || out.Data == nil {
			t.Fatal("owned assignment Run", e)
		}
	}
	return refresh
}
