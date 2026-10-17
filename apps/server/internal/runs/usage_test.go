package runs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

func testRunUsage(t *testing.T, s *Service, server *httptest.Server, key []byte) {
	t.Helper()
	ctx := context.Background()
	_, err := s.Store.Database().Exec(ctx, `INSERT INTO access_workspaces VALUES('run-usage','metering','admin');
 INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('run-usage','admin','admin',true),('run-usage','viewer','viewer',false);
 INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('usage-one','run-usage','admin','one','one','first',true,true),('usage-two','run-usage','admin','two','two','first',true,true)`)
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, device := range []string{"usage-one", "usage-two"} {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte("device\x00" + device + "\x00first"))
		tokens[device] = "d." + device + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	serial := 0
	post := func(actor, path string, value map[string]any, want int) map[string]any {
		t.Helper()
		serial++
		if value["operation_id"] == nil {
			value["operation_id"] = fmt.Sprintf("usage-op-%d", serial)
		}
		if value["expected_version"] == nil {
			value["expected_version"] = "0"
		}
		r, _ := http.NewRequest("POST", server.URL+"/runs/workspaces/run-usage/"+path, bytes.NewReader(raw(value)))
		if token := tokens[actor]; token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		} else {
			r.Header.Set("X-Fixture-User", actor)
		}
		res, e := server.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s: %d, want %d: %s", path, res.StatusCode, want, b)
		}
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		if want == 200 {
			return out["data"].(map[string]any)
		}
		return nil
	}
	created := post("admin", "executions/metered/create", map[string]any{"spec": Spec{Input: Input{Command: Command{Kind: "cli", Target: "usage-fixture"}}}}, 200)
	post("usage-one", "runner", map[string]any{"runner_id": "usage-runner", "connections": []Connection{{Kind: "cli", Target: "usage-fixture", Version: "fixture-v1", Environment: "windows", Subject: "fixture", Confirmed: true, Users: []string{"admin"}}}}, 200)
	assigned := post("admin", "executions/metered/assign", map[string]any{"expected_version": created["version"], "device_id": "usage-one"}, 200)
	read := func() Record {
		r, e := s.Read(ctx, access.Principal{UserID: "admin"}, "run-usage", Ref{ID: "run:metered", Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	var run Run
	_ = json.Unmarshal(read().Value, &run)
	generation := run.Assignment.Generation
	version := assigned["version"]
	for n, kind := range []string{"prepared", "started", "finished"} {
		v := map[string]any{"expected_version": version, "generation": generation, "sequence": fmt.Sprint(n + 1), "type": kind}
		if kind == "finished" {
			v["exit_code"] = 0
		}
		ref := post("usage-one", "executions/metered/report", v, 200)
		version = ref["version"]
	}
	before := read()
	observation := map[string]any{
		"source": "device:usage-one/codex-jsonl", "transform_version": "codex-0.162.1/v1", "event_id": "turn-one-final", "series_id": "owned-session/turn-one/model", "sequence": 1, "mode": "cumulative", "coverage_group": "run:metered/model", "external_request_id": "owned-session/turn-one/model", "covers_requests": []string{}, "coverage": "confirmed", "connection": "usage-fixture", "actual_model": "model-fixed", "at": time.Now().UTC(), "attribution": map[string]any{"kind": "run", "id": "run:metered", "resource": "run:metered", "scopes": []string{}}, "quantities": map[string]any{"input": map[string]any{"value": "10", "state": "reported", "unit": "token"}},
		"measurement": map[string]any{"state": "final", "protocol": "codex-jsonl", "cli_version": "fixture-v1", "session": "owned-session", "generation": generation, "billing": "subscription", "scope": "main-session", "source_ref": "runner-log://usage-one/owned/1", "source_digest": fmt.Sprintf("%064d", 1)},
	}
	req := map[string]any{"operation_id": "usage-stable", "generation": generation, "value": observation}
	ref := post("usage-one", "executions/metered/usage", req, 200)
	if got := post("usage-one", "executions/metered/usage", req, 200); got["version"] != ref["version"] {
		t.Fatal("replay changed usage")
	}
	if !bytes.Equal(before.Value, read().Value) {
		t.Fatal("late usage changed terminal Run")
	}
	for _, actor := range []string{"admin", "usage-two"} {
		post(actor, "executions/metered/usage", map[string]any{"generation": generation, "value": observation}, 403)
	}
	post("usage-one", "executions/metered/usage", map[string]any{"generation": "wrong", "value": observation}, 403)
	clone := func(v map[string]any) map[string]any {
		var out map[string]any
		_ = json.Unmarshal(raw(v), &out)
		return out
	}
	secondModel := clone(observation)
	secondModel["series_id"], secondModel["event_id"], secondModel["coverage_group"], secondModel["external_request_id"], secondModel["actual_model"] = "second-model", "second-model-event", "second-model-group", "second-model-request", "model-two"
	post("usage-one", "executions/metered/usage", map[string]any{"generation": generation, "value": secondModel}, 200)
	us := usage.New(s.Store, s.Access)
	q := usage.Request{Mode: "run", Kind: "usage", AttributionID: "run:metered", Limit: 1}
	page, e := us.Query(ctx, access.Principal{UserID: "viewer"}, "run-usage", q)
	if e != nil || page.Data == nil || len(page.Data.Items) != 1 {
		t.Fatal("Run usage page", page, e)
	}
	if bytes.Contains(page.Data.Items[0], []byte("api_key")) {
		t.Fatal("credential in usage")
	}
	var visible struct {
		Refs []usage.Ref `json:"refs"`
	}
	_ = json.Unmarshal(page.Data.Items[0], &visible)
	for _, ref := range visible.Refs {
		if len(ref.ID) >= 4 && ref.ID[:4] == "run:" {
			t.Fatal("internal Run/input authorization dependencies exposed as usage provenance")
		}
	}
	if page.Meta.NextCursor == nil {
		t.Fatal("second model page missing")
	}
	q.Cursor = *page.Meta.NextCursor
	next, e := us.Query(ctx, access.Principal{UserID: "viewer"}, "run-usage", q)
	if e != nil || next.Data == nil || len(next.Data.Items) != 1 || bytes.Equal(next.Data.Items[0], page.Data.Items[0]) {
		t.Fatal("Run cursor repeated first model", e)
	}
	q.Cursor = ""
	_, e = s.Store.Database().Exec(ctx, `UPDATE access_resources SET restricted=true WHERE workspace_id='run-usage' AND id='run:metered'`)
	if e != nil {
		t.Fatal(e)
	}
	page, e = us.Query(ctx, access.Principal{UserID: "viewer"}, "run-usage", q)
	if e != nil || page.Data == nil || len(page.Data.Items) != 0 {
		t.Fatal("private Run usage leaked", page, e)
	}
	for _, outcome := range []string{"failed", "cancelled", "unknown"} {
		id := "metered-" + outcome
		created := post("admin", "executions/"+id+"/create", map[string]any{"spec": Spec{Input: Input{Command: Command{Kind: "cli", Target: "usage-fixture"}}}}, 200)
		ref := post("admin", "executions/"+id+"/assign", map[string]any{"expected_version": created["version"], "device_id": "usage-one"}, 200)
		r, e := s.Read(ctx, access.Principal{UserID: "admin"}, "run-usage", Ref{ID: "run:" + id, Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		var run Run
		_ = json.Unmarshal(r.Value, &run)
		gen := run.Assignment.Generation
		for n, kind := range []string{"prepared", "started", outcome} {
			v := map[string]any{"expected_version": ref["version"], "generation": gen, "sequence": fmt.Sprint(n + 1), "type": kind}
			if outcome == "failed" && n == 2 {
				v["type"] = "finished"
				v["exit_code"] = 1
			}
			ref = post("usage-one", "executions/"+id+"/report", v, 200)
		}
		before, e := s.Read(ctx, access.Principal{UserID: "admin"}, "run-usage", Ref{ID: "run:" + id, Version: "head"})
		if e != nil {
			t.Fatal(e)
		}
		value := clone(observation)
		value["series_id"], value["coverage_group"], value["external_request_id"], value["event_id"] = id, id, id, id
		value["attribution"] = map[string]any{"kind": "run", "id": "run:" + id, "resource": "run:" + id, "scopes": []string{}}
		value["measurement"].(map[string]any)["generation"] = gen
		value["measurement"].(map[string]any)["state"] = "partial"
		post("usage-one", "executions/"+id+"/usage", map[string]any{"generation": gen, "value": value}, 200)
		after, e := s.Read(ctx, access.Principal{UserID: "admin"}, "run-usage", Ref{ID: "run:" + id, Version: "head"})
		if e != nil || !bytes.Equal(before.Value, after.Value) {
			t.Fatal("usage changed outcome", outcome, e)
		}
	}
	t.Log("terminal Run usage, stable replay, wrong reporter/generation denial, Run page and current privacy passed")
}
