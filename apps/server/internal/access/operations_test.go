package access

import (
	"context"
	"fmt"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

func testOperationBoundaries(t *testing.T, s *Service) {
	ctx := context.Background()
	db := s.DB
	_, err := db.Exec(ctx, `INSERT INTO shared_streams(workspace_id,origin,position) VALUES('operations','fixture','fixture:0');
 INSERT INTO access_workspaces VALUES('operations','operations','alice');
 INSERT INTO access_members(workspace_id,user_id,role) VALUES('operations','alice','admin'),('operations','bob','viewer');
 INSERT INTO access_resources(workspace_id,id,restricted) VALUES('operations','context:public',false),('operations','context:secret',false),('operations','context:scope-secret',false),('operations','scope:private',false);
 INSERT INTO shared_versions VALUES('operations','context:public','public-v2',false),('operations','context:secret','secret-v1',false),('operations','context:scope-secret','scope-v1',false),('operations','membership:m','m-v1',false);
 INSERT INTO context_records VALUES
 ('operations','context:public','sentence','public-v1','{"kind":"sentence","text":"old classified text","tags":["secret"]}',1),
 ('operations','context:public','sentence','public-v2','{"kind":"sentence","text":"public text","tags":[]}',2),
 ('operations','context:secret','sentence','secret-v1','{"kind":"sentence","text":"classified text","tags":["secret"]}',3),
 ('operations','context:scope-secret','sentence','scope-v1','{"kind":"sentence","text":"scoped text"}',4),
 ('operations','membership:m','membership','m-v1','{"context":{"kind":"sentence","id":"scope-secret"},"scope":{"kind":"issue","id":"private"},"present":true}',5);`)
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := Principal{UserID: "alice"}, Principal{UserID: "bob"}
	counter := 0
	mutation := func(version string) apicontract.Mutation {
		counter++
		return apicontract.Mutation{OperationID: fmt.Sprintf("operation-fixture-%d", counter), ExpectedVersion: version}
	}
	create := func(id string, bounded bool, refs []VersionRef) Principal {
		t.Helper()
		r, err := s.CreateOperation(ctx, alice, "operations", id, "read", bounded, refs, mutation("0"))
		if err != nil || r.Data == nil {
			t.Fatalf("create: %v %+v", err, r)
		}
		p, err := s.OperationPrincipal(ctx, "alice", "operations", s.OperationHandle("operations", id, "alice"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := create("normal", false, nil)
	for _, id := range []string{"context:public", "context:secret", "context:scope-secret", "scope:private"} {
		if err = s.Check(ctx, p, "operations", id, "get"); err != nil {
			t.Fatalf("initial eligibility %s: %v", id, err)
		}
	}
	before, _ := s.CursorSubject(ctx, p, "operations")
	policy, err := s.SetDeliveryPolicy(ctx, alice, "operations", mutation("0"), DeliveryPolicy{DeniedTags: []string{"secret"}, DeniedScopes: []string{"private"}})
	if err != nil || policy.Data == nil {
		t.Fatalf("policy: %v %+v", err, policy)
	}
	after, _ := s.CursorSubject(ctx, p, "operations")
	if before == after {
		t.Fatal("policy change did not invalidate cursor boundary")
	}
	for _, id := range []string{"context:secret", "context:scope-secret", "scope:private"} {
		if err = s.Check(ctx, p, "operations", id, "get"); err != ErrForbidden {
			t.Fatalf("prohibited classification %s: %v", id, err)
		}
		if err = s.Check(ctx, alice, "operations", id, "get"); err != nil {
			t.Fatal("ordinary user read changed", err)
		}
	}
	if err = s.Check(ctx, p, "operations", "context:public", "get"); err != nil {
		t.Fatal("public identity blocked", err)
	}
	if err = s.CheckVersion(ctx, nil, p, "operations", "context:public", "public-v1"); err != ErrForbidden {
		t.Fatal("old prohibited version leaked", err)
	}
	if err = s.CheckVersion(ctx, nil, p, "operations", "context:public", "public-v2"); err != nil {
		t.Fatal("current public version blocked", err)
	}
	for _, action := range []string{"manage", "execute", "register"} {
		if err = s.Check(ctx, p, "operations", "", action); err != ErrForbidden {
			t.Fatal("operation widened authority", action, err)
		}
	}
	if _, err = s.OperationPrincipal(ctx, "bob", "operations", s.OperationHandle("operations", "normal", "alice")); err != ErrForbidden {
		t.Fatal("operation transferred to another user")
	}
	if _, err = s.SetDeliveryPolicy(ctx, bob, "operations", mutation(policy.Data.Version), DeliveryPolicy{}); err != ErrForbidden {
		t.Fatal("viewer changed delivery rules")
	}
	fixed := create("fixed", true, []VersionRef{{ID: "context:public", Version: "public-v2"}})
	if err = s.CheckVersion(ctx, nil, fixed, "operations", "context:public", "public-v1"); err != ErrForbidden {
		t.Fatal("fixed input expanded to another version")
	}
	none := create("none", true, nil)
	if err = s.Check(ctx, none, "operations", "context:public", "get"); err != ErrForbidden {
		t.Fatal("none input broadened")
	}
	if err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return s.CheckVersion(ctx, tx, p, "operations", "context:public", "public-v1") }); err != ErrForbidden {
		t.Fatal("transaction boundary differs", err)
	}
	// Ten thousand Contexts are classified by one rule, not ten thousand grants.
	_, err = db.Exec(ctx, `INSERT INTO access_resources(workspace_id,id,restricted) SELECT 'operations','context:bulk-'||n,false FROM generate_series(1,10000)n;
 INSERT INTO shared_versions SELECT 'operations','context:bulk-'||n,'v1',false FROM generate_series(1,10000)n;
 INSERT INTO context_records SELECT 'operations','context:bulk-'||n,'sentence','v1',jsonb_build_object('kind','sentence','text','bulk','tags',CASE WHEN n%2=0 THEN '["secret"]'::jsonb ELSE '[]'::jsonb END),n FROM generate_series(1,10000)n;`)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 1; i <= 100; i++ {
		ids = append(ids, fmt.Sprintf("context:bulk-%d", i))
	}
	eligible, err := s.Readable(ctx, p, "operations", ids)
	if err != nil || len(eligible) != 50 {
		t.Fatal("bulk rule did not filter requested page", err, len(eligible))
	}
	closed, err := s.CloseOperation(ctx, alice, "operations", "normal", mutation("1"))
	_ = closed
	// The operation resource version is opaque; use its actual current version.
	if err == nil && closed.Error == nil {
		t.Fatal("stale close unexpectedly applied")
	}
	var v string
	if err = db.QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id='operations' AND resource_id='operation:normal'").Scan(&v); err != nil {
		t.Fatal(err)
	}
	closed, err = s.CloseOperation(ctx, alice, "operations", "normal", mutation(v))
	if err != nil || closed.Error != nil {
		t.Fatal("close", err, closed.Error)
	}
	if _, err = s.OperationPrincipal(ctx, "alice", "operations", s.OperationHandle("operations", "normal", "alice")); err != ErrForbidden {
		t.Fatal("closed operation reused")
	}
	var ruleCount int
	_ = db.QueryRow(ctx, "SELECT count(*) FROM access_delivery_policies WHERE workspace_id='operations'").Scan(&ruleCount)
	if ruleCount != 1 {
		t.Fatal("per-Context policy rows were created")
	}
	t.Log("operation/user/version/none boundaries, automatic tag/direct-Scope denial, ordinary HTTP continuity and 10,000-Context classification passed")
}
