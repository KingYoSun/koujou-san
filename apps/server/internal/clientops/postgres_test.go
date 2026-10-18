package clientops

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

func TestClientOperationsPostgres(t *testing.T) {
	url := os.Getenv("KOUJOU_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("run pnpm run test:clientops")
	}
	ctx := context.Background()
	store, err := persistence.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.Database().Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO auth_users VALUES('alice','owned','alice','alice@koujou.test','Alice',false),('bob','owned','bob','bob@koujou.test','Bob',false),('other','owned','other','other@koujou.test','Other',false);INSERT INTO access_workspaces VALUES('one','one','alice'),('two','two','other');INSERT INTO access_members(workspace_id,user_id,role,can_execute) VALUES('one','alice','admin',false),('one','bob','viewer',false),('two','other','admin',false)`)
	a := access.New(store, []byte(strings.Repeat("a", 32)), func(http.ResponseWriter, *http.Request) (string, bool) { return "alice", true })
	c := contexts.New(store, a, contexts.DefaultLimits())
	s := New(store, a, c, usage.New(store, a))
	p := access.Principal{UserID: "alice"}
	m := func(id, version string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: id, ExpectedVersion: version}
	}
	accepted := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		t.Helper()
		if err != nil || v.Error != nil || v.Data == nil {
			t.Fatalf("not accepted: %+v %v", v, err)
		}
		return Ref{ID: v.Data.ID, Version: v.Data.Version}
	}
	text := "fixed original"
	v := accepted(c.Put(ctx, p, "one", contexts.Identity{Kind: "sentence", ID: "one"}, contexts.Write{Mutation: m("source", "0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
	source := contexts.Ref{Kind: "sentence", ID: "one", Revision: v.Version}
	t.Run("manual_save_and_replay_share_one_manifest_without_Run", func(t *testing.T) {
		if _, err := c.Put(ctx, p, "one", contexts.Identity{Kind: "manifest", ID: "ordinary-still-needs-run"}, contexts.Write{Mutation: m("ordinary-manifest", "0"), Value: raw(contexts.ResolveRequest{Selection: contexts.Selection{Mode: "none"}})}); err == nil {
			t.Fatal("ordinary Manifest contract changed")
		}
		req := SelectionWrite{Mutation: m("manual-selection", "0"), Input: contexts.Selection{Mode: "select", Include: []contexts.Ref{source}}, Items: []Item{{Ref: source, Reason: "manual"}}}
		ref := accepted(s.PutSelection(ctx, p, "one", "client-selection:one", req))
		again := accepted(s.PutSelection(ctx, p, "one", "client-selection:one", req))
		if ref != again {
			t.Fatal("replay changed version")
		}
		r, err := s.Read(ctx, p, "one", ref)
		if err != nil {
			t.Fatal(err)
		}
		var chosen Selection
		_ = json.Unmarshal(r.Value, &chosen)
		manifest, err := c.Read(ctx, p, "one", chosen.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		var resolved contexts.Manifest
		_ = json.Unmarshal(manifest.Value, &resolved)
		if resolved.RunID != "" || len(resolved.Placements) != 1 || resolved.Placements[0].Ref != source {
			t.Fatal(resolved)
		}
		var runs, manifests int
		if err = store.Database().QueryRow(ctx, `SELECT (SELECT count(*) FROM run_records),(SELECT count(*) FROM context_records WHERE kind='manifest')`).Scan(&runs, &manifests); err != nil || runs != 0 || manifests != 1 {
			t.Fatal(runs, manifests, err)
		}
		stale := req
		stale.Mutation = m("stale", "0")
		out, err := s.PutSelection(ctx, p, "one", ref.ID, stale)
		if err != nil || out.Error == nil || out.Error.Code != "version_conflict" {
			t.Fatal(out, err)
		}
	})
	t.Run("policy_and_selection_keep_current_authority", func(t *testing.T) {
		policy := accepted(s.PutPolicy(ctx, p, "one", "selection-policy:one", Write{Mutation: m("policy", "0"), Value: raw(DefaultPolicy())}))
		if _, err := s.Read(ctx, access.Principal{UserID: "other"}, "one", policy); err == nil {
			t.Fatal("other Workspace read")
		}
		if _, err := s.PutSelection(ctx, access.Principal{UserID: "bob"}, "one", "client-selection:denied", SelectionWrite{Mutation: m("denied", "0"), Input: contexts.Selection{Mode: "none"}}); err == nil {
			t.Fatal("viewer write")
		}
	})
	t.Run("MCP_read_does_not_grant_selection_write", func(t *testing.T) {
		accepted(a.CreateOperation(ctx, p, "one", "read", "read", false, nil, m("read-operation", "0")))
		if _, err := s.PutSelection(ctx, access.Principal{UserID: "alice", OperationID: "read"}, "one", "client-selection:read-denied", SelectionWrite{Mutation: m("read-denied", "0"), Input: contexts.Selection{Mode: "none"}}); err == nil {
			t.Fatal("read operation wrote")
		}
		accepted(a.CreateOperation(ctx, p, "one", "edit", "selection-edit", false, nil, m("edit-operation", "0")))
		accepted(s.PutSelection(ctx, access.Principal{UserID: "alice", OperationID: "edit"}, "one", "client-selection:mcp", SelectionWrite{Mutation: m("mcp-selection", "0"), Input: contexts.Selection{Mode: "select", Include: []contexts.Ref{source}}}))
	})
	t.Run("Scope_bounds_do_not_override_classification_or_expand_to_Workspace", func(t *testing.T) {
		accepted(c.Put(ctx, p, "one", contexts.Identity{Kind: "scope", ID: "project"}, contexts.Write{Mutation: m("scope", "0"), Value: raw(contexts.Scope{Kind: "project", Name: "owned project"})}))
		accepted(c.Put(ctx, p, "one", contexts.Identity{Kind: "membership", ID: "in-project"}, contexts.Write{Mutation: m("membership", "0"), Value: raw(contexts.Membership{Scope: contexts.Identity{Kind: "project", ID: "project"}, Context: source.Identity(), Present: true})}))
		outside := accepted(c.Put(ctx, p, "one", contexts.Identity{Kind: "sentence", ID: "outside"}, contexts.Write{Mutation: m("outside", "0"), Value: raw(contexts.Unit{Kind: "sentence", Text: &text})}))
		accepted(a.CreateOperation(ctx, p, "one", "scoped", "read", true, nil, m("scoped-operation", "0")))
		principal := access.Principal{UserID: "alice", OperationID: "scoped"}
		if _, err := c.Read(ctx, principal, "one", source); err == nil {
			t.Fatal("empty bounded operation read a Context")
		}
		exec(`INSERT INTO access_operation_scopes VALUES('one','scoped','project')`)
		if _, err := c.Read(ctx, principal, "one", source); err != nil {
			t.Fatal("direct Scope candidate denied", err)
		}
		if _, err := c.Read(ctx, principal, "one", contexts.Ref{Kind: "sentence", ID: "outside", Revision: outside.Version}); err == nil {
			t.Fatal("Scope expanded to all Contexts")
		}
		accepted(a.SetDeliveryPolicy(ctx, p, "one", m("delivery-policy", "0"), access.DeliveryPolicy{DeniedTags: []string{"no-export"}}))
		accepted(c.Put(ctx, p, "one", source.Identity(), contexts.Write{Mutation: m("classify", source.Revision), Value: raw(contexts.Unit{Kind: "sentence", Text: &text, Tags: []string{"no-export"}})}))
		if _, err := c.Read(ctx, principal, "one", source); err == nil {
			t.Fatal("current classification bypassed through Scope")
		}
	})
	t.Run("client_operation_ownership_replay_pause_limits_and_usage", func(t *testing.T) { checkOperations(t, s) })
	t.Run("generated_definitions_use_normal_validation_and_never_start", func(t *testing.T) { checkDefinitions(t, s) })
}
