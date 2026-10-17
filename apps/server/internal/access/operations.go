package access

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/json"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

// DeliveryPolicy classifies prohibited information using existing tags and direct
// memberships. Rules are evaluated against bounded candidates, never by exporting
// an allow-list of every Context in a Workspace.
type DeliveryPolicy struct {
	DeniedTags   []string `json:"denied_tags"`
	DeniedScopes []string `json:"denied_scopes"`
}
type VersionRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Operation struct {
	ID      string `json:"id"`
	Purpose string `json:"purpose"`
	Bounded bool   `json:"bounded"`
	Closed  bool   `json:"closed"`
}

func (s *Service) DeliveryPolicy(ctx context.Context, p Principal, w string) (DeliveryPolicy, string, error) {
	if err := s.Check(ctx, p, w, "", "manage"); err != nil {
		return DeliveryPolicy{}, "", err
	}
	var raw []byte
	var version string
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(p.payload,'{"denied_tags":[],"denied_scopes":[]}'::jsonb),COALESCE(v.version,'0') FROM access_workspaces w LEFT JOIN access_delivery_policies p ON p.workspace_id=w.id LEFT JOIN shared_versions v ON v.workspace_id=w.id AND v.resource_id='llm-policy' WHERE w.id=$1`, w).Scan(&raw, &version)
	var value DeliveryPolicy
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value, version, err
}
func (s *Service) SetDeliveryPolicy(ctx context.Context, p Principal, w string, m apicontract.Mutation, value DeliveryPolicy) (apicontract.Response[apicontract.Reference], error) {
	if value.DeniedTags == nil {
		value.DeniedTags = []string{}
	}
	if value.DeniedScopes == nil {
		value.DeniedScopes = []string{}
	}
	return s.mutate(ctx, p, w, "llm-policy", "delivery-policy", "manage", m, value, func(tx pgx.Tx) (any, error) {
		for _, id := range value.DeniedScopes {
			if err := s.CheckTx(ctx, tx, p, w, "scope:"+id, "get"); err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, "INSERT INTO access_delivery_policies VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET payload=EXCLUDED.payload", w, raw)
		return value, err
	})
}
func (s *Service) Operation(ctx context.Context, p Principal, w, id string) (Operation, error) {
	return s.operation(ctx, s.DB, p, w, id)
}
func (s *Service) operation(ctx context.Context, q query, p Principal, w, id string) (Operation, error) {
	var op Operation
	err := q.QueryRow(ctx, "SELECT id,purpose,bounded,closed FROM access_operations WHERE workspace_id=$1 AND id=$2 AND user_id=$3", w, id, p.UserID).Scan(&op.ID, &op.Purpose, &op.Bounded, &op.Closed)
	if err == nil && op.Closed {
		err = ErrForbidden
	}
	return op, deny(err)
}
func (s *Service) OperationHandle(w, id, user string) string {
	return id + "." + s.proof("operation", w+":"+id, user)
}
func (s *Service) OperationPrincipal(ctx context.Context, user, w, handle string) (Principal, error) {
	id, proof, ok := tokenParts(handle)
	p := Principal{UserID: user}
	if !ok || !hmac.Equal([]byte(proof), []byte(s.proof("operation", w+":"+id, user))) {
		return p, ErrForbidden
	}
	if _, err := s.Operation(ctx, p, w, id); err != nil {
		return p, err
	}
	p.OperationID = id
	if err := s.Check(ctx, p, w, "", "read"); err != nil {
		return Principal{}, err
	}
	return p, nil
}

// CreateOperation receives already-authorized Manifest placements from the
// owning Context service. A bounded empty set is meaningful: Run.input=none.
func (s *Service) CreateOperation(ctx context.Context, p Principal, w, id, purpose string, bounded bool, refs []VersionRef, m apicontract.Mutation) (apicontract.Response[apicontract.Reference], error) {
	if purpose != "read" && purpose != "edit" && purpose != "price-edit" && purpose != "triage-edit" && purpose != "triage-manage" && purpose != "hopper-edit" && purpose != "hopper-manage" {
		return apicontract.Response[apicontract.Reference]{}, ErrForbidden
	}
	if refs == nil {
		refs = []VersionRef{}
	}
	action := "read"
	if purpose == "edit" || purpose == "price-edit" || purpose == "triage-edit" || purpose == "hopper-edit" {
		action = "register"
	}
	if purpose == "triage-manage" || purpose == "hopper-manage" {
		action = "manage"
	}
	return s.mutate(ctx, p, w, "operation:"+id, "llm-operation", action, m, struct {
		Purpose string
		Bounded bool
		Refs    []VersionRef
	}{purpose, bounded, refs}, func(tx pgx.Tx) (any, error) {
		if _, err := tx.Exec(ctx, "INSERT INTO access_operations(workspace_id,id,user_id,purpose,bounded) VALUES($1,$2,$3,$4,$5)", w, id, p.UserID, purpose, bounded); err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(refs)
		_, err := tx.Exec(ctx, `INSERT INTO access_operation_refs SELECT $1,$2,r.id,r.version FROM jsonb_to_recordset($3::jsonb) AS r(id text,version text) ON CONFLICT DO NOTHING`, w, id, raw)
		return Operation{ID: id, Purpose: purpose, Bounded: bounded}, err
	})
}
func (s *Service) CloseOperation(ctx context.Context, p Principal, w, id string, m apicontract.Mutation) (apicontract.Response[apicontract.Reference], error) {
	var owner string
	if err := s.DB.QueryRow(ctx, "SELECT user_id FROM access_operations WHERE workspace_id=$1 AND id=$2", w, id).Scan(&owner); err != nil || owner != p.UserID {
		return apicontract.Response[apicontract.Reference]{}, ErrForbidden
	}
	return s.mutate(ctx, p, w, "operation:"+id, "llm-operation", "read", m, struct{ Closed bool }{true}, func(tx pgx.Tx) (any, error) {
		_, err := tx.Exec(ctx, "UPDATE access_operations SET closed=true WHERE workspace_id=$1 AND id=$2 AND user_id=$3", w, id, p.UserID)
		return struct{ Closed bool }{true}, err
	})
}

// CursorSubject binds projection cursors to the operation and current policy.
// Ordinary GUI/HTTP cursors retain their original subject and wire format.
func (s *Service) CursorSubject(ctx context.Context, p Principal, w string) (string, error) {
	if p.OperationID == "" {
		return p.UserID, nil
	}
	if _, err := s.Operation(ctx, p, w, p.OperationID); err != nil {
		return "", err
	}
	var version string
	err := s.DB.QueryRow(ctx, "SELECT COALESCE((SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id='llm-policy'),'0')", w).Scan(&version)
	return p.UserID + ":" + p.OperationID + ":" + version, err
}

// versionGrants checks only requested versions. Both fixed-version tags and
// current identity classification apply to historical reads.
func versionGrants(ctx context.Context, q query, p Principal, w string, refs []VersionRef) (map[VersionRef]bool, error) {
	out := map[VersionRef]bool{}
	if p.OperationID == "" {
		for _, r := range refs {
			out[r] = true
		}
		return out, nil
	}
	raw, _ := json.Marshal(refs)
	rows, err := q.Query(ctx, versionQuery("$1", "$2", "$3", "$4"), w, p.OperationID, p.UserID, raw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref VersionRef
		if err = rows.Scan(&ref.ID, &ref.Version); err != nil {
			return nil, err
		}
		out[ref] = true
	}
	return out, rows.Err()
}
func (s *Service) Versions(ctx context.Context, p Principal, w string, refs []VersionRef) (map[VersionRef]bool, error) {
	return versionGrants(ctx, s.DB, p, w, refs)
}
func (s *Service) CheckVersion(ctx context.Context, tx pgx.Tx, p Principal, w, id, version string) error {
	if p.OperationID == "" {
		return nil
	}
	var q query = s.DB
	if tx != nil {
		q = tx
	}
	ref := VersionRef{id, version}
	allowed, err := versionGrants(ctx, q, p, w, []VersionRef{ref})
	if err != nil {
		return err
	}
	if !allowed[ref] {
		return ErrForbidden
	}
	return nil
}
func operationAction(op Operation, action string) bool {
	return action == "read" || action == "get" || action == "search" || (action == "update" && op.Purpose == "edit") || (action == "price_write" && op.Purpose == "price-edit") || (action == "triage_write" && (op.Purpose == "triage-edit" || op.Purpose == "triage-manage")) || (action == "triage_manage" && op.Purpose == "triage-manage") || (action == "hopper_write" && (op.Purpose == "hopper-edit" || op.Purpose == "hopper-manage")) || (action == "hopper_manage" && op.Purpose == "hopper-manage")
}
func identityVersionRefs(ids []string) []VersionRef {
	refs := make([]VersionRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, VersionRef{ID: id})
	}
	return refs
}

// OperationEvent is deliberately content-free: IDs, fixed versions and result
// boundaries are enough to inspect delivery without persisting conversations.
type OperationEvent struct {
	Refs       []VersionRef     `json:"refs"`
	Range      string           `json:"range"`
	Meta       apicontract.Meta `json:"meta"`
	MutationID string           `json:"mutation_id,omitempty"`
	Outcome    string           `json:"outcome"`
}

func (s *Service) RecordOperation(ctx context.Context, p Principal, w, method, boundary string, event OperationEvent) error {
	if err := s.Check(ctx, p, w, "", "read"); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO access_operation_events(workspace_id,operation_id,id,user_id,method,boundary,result) VALUES($1,$2,$3,$4,$5,$6,$7)`, w, p.OperationID, rand.Text(), p.UserID, method, boundary, raw)
	return err
}

// versionQuery is shared by fixed reads and server-side cost aggregation.
func versionQuery(w, op, user, refs string) string {
	const query = `WITH wanted AS (SELECT * FROM jsonb_to_recordset(@refs::jsonb) AS r(id text,version text))
 SELECT r.id,r.version FROM wanted r JOIN access_operations o ON o.workspace_id=@workspace AND o.id=@operation AND o.user_id=@user AND NOT o.closed
 LEFT JOIN access_delivery_policies p ON p.workspace_id=@workspace
 LEFT JOIN shared_versions v ON v.workspace_id=@workspace AND v.resource_id=r.id
 LEFT JOIN LATERAL (SELECT payload FROM context_records WHERE workspace_id=@workspace AND resource_id=r.id AND revision=v.version LIMIT 1) c ON true
 LEFT JOIN LATERAL (SELECT payload FROM context_records WHERE workspace_id=@workspace AND resource_id=r.id AND revision=r.version LIMIT 1) f ON true
 WHERE (NOT o.bounded OR EXISTS(SELECT 1 FROM access_operation_refs a WHERE a.workspace_id=@workspace AND a.operation_id=o.id AND a.resource_id=r.id AND (r.version='' OR a.version=r.version)))
 AND NOT (COALESCE(c.payload->'tags','[]'::jsonb) ?| ARRAY(SELECT jsonb_array_elements_text(COALESCE(p.payload->'denied_tags','[]'::jsonb))))
 AND NOT (COALESCE(f.payload->'tags','[]'::jsonb) ?| ARRAY(SELECT jsonb_array_elements_text(COALESCE(p.payload->'denied_tags','[]'::jsonb))))
 AND NOT EXISTS(SELECT 1 FROM context_records m JOIN shared_versions mv ON mv.workspace_id=m.workspace_id AND mv.resource_id=m.resource_id AND mv.version=m.revision AND NOT mv.deleted
 WHERE r.id LIKE 'context:%' AND m.workspace_id=@workspace AND m.kind='membership' AND m.payload->'context'->>'id'=substring(r.id from 9) AND m.payload->>'present'='true' AND p.payload->'denied_scopes' ? (m.payload->'scope'->>'id'))
 AND NOT (r.id LIKE 'scope:%' AND COALESCE(p.payload->'denied_scopes','[]'::jsonb) ? substring(r.id from 7))`
	return strings.NewReplacer("@workspace", w, "@operation", op, "@user", user, "@refs", refs).Replace(query)
}

// ReadFilterSQL checks all provenance refs before grouping, counting or export.
// Only trusted SQL expressions are passed by domain services. Parameters are
// $1 Workspace, $2 authenticated user, $3 operation (empty for ordinary HTTP).
func ReadFilterSQL(refs string) string {
	return `NOT EXISTS(SELECT 1 FROM jsonb_to_recordset(` + refs + `) AS need(id text,version text)
 LEFT JOIN access_resources ar ON ar.workspace_id=$1 AND ar.id=need.id
 LEFT JOIN access_resource_grants ag ON ag.workspace_id=ar.workspace_id AND ag.resource_id=ar.id AND ag.user_id=$2
 WHERE ar.id IS NULL OR (ar.restricted AND NOT COALESCE(ag.can_read,false)) OR (need.id LIKE 'github:%' AND NOT EXISTS(SELECT 1 FROM github_bindings gb WHERE gb.workspace_id=$1 AND gb.target_id=substring(need.id from 8))))
 AND ($3='' OR NOT EXISTS(SELECT 1 FROM jsonb_to_recordset(` + refs + `) AS need(id text,version text)
 WHERE NOT EXISTS(SELECT 1 FROM (` + versionQuery("$1", "$3", "$2", refs) + `) permitted WHERE permitted.id=need.id AND permitted.version=need.version)))`
}
