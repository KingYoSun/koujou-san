package access

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func TestAccessPostgres(t *testing.T) {
	database := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("run pnpm run test:access")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alice", "bob", "outsider"} {
		if _, err = store.Database().Exec(ctx, "INSERT INTO auth_users VALUES($1,'fixture',$1,$1||'@koujou.test',$1,false)", id); err != nil {
			t.Fatal(err)
		}
	}
	service := New(store, []byte(strings.Repeat("k", 32)), func(w http.ResponseWriter, r *http.Request) (string, bool) {
		id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if id == "" {
			id = r.Header.Get("X-Test-Cookie-User")
		}
		if id != "alice" && id != "bob" && id != "outsider" {
			send(w, 401, map[string]string{"error": "authentication_required"})
			return "", false
		}
		return id, true
	})
	mux := http.NewServeMux()
	service.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	request := func(actor, method, path string, body any, want int) map[string]any {
		t.Helper()
		var raw io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			raw = bytes.NewReader(data)
		}
		req, _ := http.NewRequest(method, server.URL+path, raw)
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			var safe map[string]any
			_ = json.Unmarshal(data, &safe)
			for _, key := range []string{"credential", "invitation"} {
				if _, ok := safe[key]; ok {
					safe[key] = "[redacted]"
				}
			}
			diagnostic, _ := json.Marshal(safe)
			t.Fatalf("%s %s: got %d want %d body=%s", method, path, response.StatusCode, want, diagnostic)
		}
		value := map[string]any{}
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	serial := 0
	mutation := func(id, version string) map[string]any {
		serial++
		return map[string]any{"id": id, "operation_id": fmt.Sprintf("operation-%d", serial), "expected_version": version}
	}
	version := func(result map[string]any) string {
		t.Helper()
		if wrapped, ok := result["result"].(map[string]any); ok {
			result = wrapped
		}
		return result["data"].(map[string]any)["version"].(string)
	}
	path := func(w, kind string) string { return "/access/workspaces/" + w + "/" + kind }
	create := func(actor, w string) {
		body := mutation(w, "0")
		body["name"] = w
		request(actor, "POST", path(w, "create"), body, 200)
	}
	create("alice", "one")
	create("bob", "two")
	request("outsider", "GET", path("one", "members"), nil, 403)
	root := request("outsider", "GET", "/access/workspaces", nil, 200)
	if len(root["items"].([]any)) != 0 {
		t.Fatal("login granted membership")
	}
	invite := func(id, role string) map[string]any {
		body := mutation(id, "0")
		body["role"] = role
		return request("alice", "POST", path("one", "invite-create"), body, 200)
	}
	invited := invite("invite-one", "viewer")
	fullToken := invited["invitation"].(string)
	token := strings.TrimPrefix(fullToken, "one.")
	body := mutation("", "0")
	body["token"] = token
	request("bob", "POST", path("one", "invite-accept"), body, 409)
	body["expected_version"] = version(invited)
	joined := request("bob", "POST", path("one", "invite-accept"), body, 200)
	request("bob", "POST", path("one", "invite-accept"), body, 200)
	request("outsider", "POST", path("one", "invite-accept"), body, 403)
	bodyBad := mutation("", version(joined))
	bodyBad["token"] = token + "wrong"
	request("outsider", "POST", path("one", "invite-accept"), bodyBad, 403)
	cancelled := invite("invite-cancel", "viewer")
	request("alice", "POST", path("one", "invite-cancel"), mutation("invite-cancel", version(cancelled)), 200)
	cancelBody := mutation("", version(cancelled))
	cancelBody["token"] = strings.TrimPrefix(cancelled["invitation"].(string), "one.")
	request("outsider", "POST", path("one", "invite-accept"), cancelBody, 403)
	expired := invite("invite-expired", "viewer")
	_, _ = store.Database().Exec(ctx, "UPDATE access_invites SET expires_at=now()-interval '1 second' WHERE id='invite-expired'")
	expireBody := mutation("", version(expired))
	expireBody["token"] = strings.TrimPrefix(expired["invitation"].(string), "one.")
	request("outsider", "POST", path("one", "invite-accept"), expireBody, 403)
	t.Log("verified-user creation, forwarded single-use invitation, replay, invalid/cancelled/expired links and non-member denial passed")

	err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		for _, r := range []Resource{{"one", "parent", false, ""}, {"one", "private-child", true, ""}, {"two", "foreign", false, ""}} {
			if err := service.PutResource(ctx, tx, r, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"read", "get", "search"} {
		request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": action}, 200)
		request("bob", "POST", path("one", "authorize"), map[string]string{"target": "private-child", "action": action}, 403)
		request("alice", "POST", path("one", "authorize"), map[string]string{"target": "foreign", "action": action}, 403)
	}
	for _, action := range []string{"update", "execute"} {
		request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": action}, 403)
	}
	memberBody := mutation("bob", "0")
	memberBody["role"] = "editor"
	memberBody["active"] = true
	updated := request("alice", "POST", path("one", "member-update"), memberBody, 200)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": "update"}, 200)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": "execute"}, 403)
	memberBody = mutation("bob", version(updated))
	memberBody["role"] = "editor"
	memberBody["active"] = true
	memberBody["can_execute"] = true
	memberResult := request("alice", "POST", path("one", "member-update"), memberBody, 200)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": "execute"}, 200)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "private-child", "action": "execute"}, 403)
	err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return service.PutResource(ctx, tx, Resource{"one", "private-child", true, ""}, []ResourceGrant{{UserID: "bob", Read: true}})
	})
	if err != nil {
		t.Fatal(err)
	}
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "private-child", "action": "get"}, 200)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "private-child", "action": "update"}, 403)
	t.Log("Workspace/resource boundary, no containment/Scope grant, role matrix and independent execution permission passed")
	for _, action := range []string{"get", "search", "update", "execute"} {
		request("bob", "POST", path("one", "authorize"), map[string]string{"action": action}, 403)
	}

	register := func(actor, w, id, identifier string) map[string]any {
		body := mutation(id, "0")
		body["identifier"] = identifier
		body["name"] = id
		return request(actor, "POST", path(w, "device-register"), body, 200)
	}
	first := register("bob", "one", "device-one", "physical-one")
	second := register("bob", "two", "device-two", "physical-one")
	third := register("alice", "one", "device-three", "physical-two")
	credential := first["credential"].(string)
	secondCredential := second["credential"].(string)
	request(credential, "POST", path("one", "contact"), mutation("", "0"), 409) // Registration already established the device version.
	contact := request(credential, "POST", path("one", "contact"), mutation("", version(first)), 200)
	request(credential, "POST", path("two", "authorize"), map[string]string{"action": "input", "target": "parent"}, 403)
	request(credential, "POST", path("one", "member-update"), memberBody, 401)
	request("bob", "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": "execute"}, 200)
	err = pgx.BeginFunc(ctx, store.Database(), func(tx pgx.Tx) error {
		return service.PutResource(ctx, tx, Resource{"one", "assigned", true, "device-one"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	request(credential, "POST", path("one", "authorize"), map[string]string{"target": "assigned", "action": "report"}, 403)
	allow := mutation("device-one", version(contact))
	allow["active"] = true
	allow["can_input"] = true
	allow["can_report"] = true
	grant := request("alice", "POST", path("one", "device-update"), allow, 200)
	for _, action := range []string{"input", "report"} {
		request(credential, "POST", path("one", "authorize"), map[string]string{"target": "assigned", "action": action}, 200)
		request(third["credential"].(string), "POST", path("one", "authorize"), map[string]string{"target": "assigned", "action": action}, 403)
		request(credential, "POST", path("one", "authorize"), map[string]string{"target": "parent", "action": action}, 403)
	}
	request(credential, "POST", path("one", "authorize"), map[string]string{"target": "assigned", "action": "update"}, 403)
	request("alice", "POST", path("one", "device-rotate"), mutation("device-one", version(grant)), 403)
	rotated := request("bob", "POST", path("one", "device-rotate"), mutation("device-one", version(grant)), 200)
	oldCredential := credential
	credential = rotated["credential"].(string)
	if credential == oldCredential {
		t.Fatal("rotation retained credential")
	}
	request(oldCredential, "POST", path("one", "authorize"), map[string]string{"action": "input", "target": "assigned"}, 403)
	request(credential, "POST", path("one", "authorize"), map[string]string{"action": "input", "target": "assigned"}, 200)
	t.Log("Device scopes, separate actor, assigned target, rotation and other-owner rejection passed")

	// A rotation that commits while an old credential waits on the stream lock
	// must prevent the queued request from using a cached generation or receipt.
	blocker, err := store.Database().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, "SELECT workspace_id FROM shared_streams WHERE workspace_id='one' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	queuedBody := mutation("", version(rotated))
	data, _ := json.Marshal(queuedBody)
	go func() {
		req, _ := http.NewRequest("POST", server.URL+path("one", "contact"), bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+credential)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()
	waiting := false
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(10 * time.Millisecond) {
		if err = store.Database().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT position,sequence%')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
	}
	if !waiting {
		_ = blocker.Rollback(ctx)
		t.Fatal("queued update did not reach stream lock")
	}
	if _, err = blocker.Exec(ctx, "UPDATE access_devices SET generation='new-generation' WHERE id='device-one'"); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got != 403 {
		t.Fatalf("queued old credential status %d", got)
	}
	generation := ""
	if err = store.Database().QueryRow(ctx, "SELECT generation FROM access_devices WHERE id='device-one'").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	credential = service.deviceToken("device-one", generation)
	revoke := mutation("device-one", version(rotated))
	revoke["active"] = false
	revoked := request("alice", "POST", path("one", "device-update"), revoke, 200)
	request(credential, "POST", path("one", "authorize"), map[string]string{"action": "input", "target": "assigned"}, 403)
	reenroll := request("bob", "POST", path("one", "device-rotate"), mutation("device-one", version(revoked)), 200)
	credential = reenroll["credential"].(string)
	request(credential, "POST", path("one", "authorize"), map[string]string{"action": "input", "target": "assigned"}, 403)
	request(credential, "POST", path("one", "contact"), mutation("", version(reenroll)), 200)
	leave := mutation("bob", version(memberResult))
	request("bob", "POST", path("one", "leave"), leave, 200)
	request("bob", "POST", path("one", "leave"), leave, 403)
	request("bob", "GET", path("one", "members"), nil, 403)
	request(credential, "POST", path("one", "authorize"), map[string]string{"action": "input", "target": "assigned"}, 403)
	request(secondCredential, "POST", path("two", "contact"), mutation("", version(second)), 200)
	shared := request("alice", "GET", path("one", "devices"), nil, 200)
	encoded, _ := json.Marshal(shared)
	if !bytes.Contains(encoded, []byte(`"enabled":false`)) || bytes.Contains(encoded, []byte(`"stopped"`)) {
		t.Fatal("revoked device or unknown execution state misrepresented")
	}
	t.Log("revocation, lock-time generation recheck, membership cancellation and other-Workspace continuity passed")

	rows, err := store.Database().Query(ctx, "SELECT payload::text FROM shared_changes UNION ALL SELECT result::text FROM shared_receipts")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var record string
		if err = rows.Scan(&record); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{token, oldCredential, credential, secondCredential} {
			if strings.Contains(record, secret) {
				t.Fatal("secret entered shared journal/receipt")
			}
		}
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	t.Log("shared responses, journal and receipts contain no private invitation/device proofs")
	t.Run("operation-boundaries", func(t *testing.T) { testOperationBoundaries(t, service) })
}
