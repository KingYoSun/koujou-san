package automation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Store  *persistence.Store
	Access *access.Service
}

func New(store *persistence.Store, a *access.Service) *Service {
	s := &Service{store, a}
	store.Accepted = s.accepted
	return s
}
func encode(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	return nil
}
func kind(id string) string { k, _, _ := strings.Cut(id, ":"); return k }
func problem(err error) *apicontract.Problem {
	if err == nil {
		return nil
	}
	code := "unavailable"
	if errors.Is(err, access.ErrForbidden) {
		code = "forbidden"
	}
	return &apicontract.Problem{Code: code}
}

func persistenceScope(p access.Principal, w string, recheck func(pgx.Tx) error) persistence.Scope {
	return persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem { return problem(recheck(tx)) }}
}
func persistenceCommand(w, id, k string, m apicontract.Mutation, raw json.RawMessage) persistence.Command {
	return persistence.Command{WorkspaceID: w, ResourceID: id, Kind: k, Mutation: m, Payload: raw}
}
func (s *Service) checkRefs(ctx context.Context, tx pgx.Tx, p access.Principal, w string, refs []Ref) error {
	for _, r := range refs {
		var err error
		if tx == nil {
			err = s.Access.Check(ctx, p, w, r.ID, "get")
		} else {
			err = s.Access.CheckTx(ctx, tx, p, w, r.ID, "get")
		}
		if err != nil {
			return err
		}
		if err = s.Access.CheckVersion(ctx, tx, p, w, r.ID, r.Version); err != nil {
			return err
		}
		if r.Version != "" {
			var q query = s.Store.Database()
			if tx != nil {
				q = tx
			}
			var exists bool
			if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2 AND version=$3
 UNION ALL SELECT 1 FROM shared_changes WHERE workspace_id=$1 AND resource_id=$2 AND version=$3
 UNION ALL SELECT 1 FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3
 UNION ALL SELECT 1 FROM automation_records WHERE workspace_id=$1 AND id=$2 AND version=$3
 UNION ALL SELECT 1 FROM usage_records WHERE workspace_id=$1 AND id=$2 AND version=$3)`, w, r.ID, r.Version).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalid
			}
		}
	}
	return nil
}
func record(ctx context.Context, q query, w string, r Ref) (Record, error) {
	var out Record
	var refs []byte
	err := q.QueryRow(ctx, `SELECT r.id,r.version,r.kind,r.position,r.payload,r.refs FROM automation_records r WHERE r.workspace_id=$1 AND r.id=$2 AND r.version=CASE WHEN $3='head' THEN (SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2) ELSE $3 END`, w, r.ID, r.Version).Scan(&out.ID, &out.Version, &out.Kind, &out.Position, &out.Value, &refs)
	if err == nil {
		err = json.Unmarshal(refs, &out.Refs)
	}
	return out, err
}
func (s *Service) Read(ctx context.Context, p access.Principal, w string, r Ref) (Record, error) {
	v, err := record(ctx, s.Store.Database(), w, r)
	if err != nil {
		return v, access.ErrForbidden
	}
	if err = s.checkRefs(ctx, nil, p, w, append(append([]Ref{}, v.Refs...), v.Ref)); err != nil {
		return Record{}, err
	}
	return v, nil
}
func (s *Service) writable(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string) error {
	var q query = s.Store.Database()
	if tx != nil {
		q = tx
	}
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2)", w, id).Scan(&exists); err != nil {
		return err
	}
	target, action := "", "register"
	if exists {
		target, action = id, "update"
	}
	if kind(id) == "control" || kind(id) == "activation" {
		target, action = "", "manage"
		if p.OperationID != "" {
			action = "triage_manage"
		}
	} else if p.OperationID != "" {
		action = "triage_write"
	}
	if tx == nil {
		return s.Access.Check(ctx, p, w, target, action)
	}
	return s.Access.CheckTx(ctx, tx, p, w, target, action)
}
func (s *Service) Put(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	if err := s.writable(ctx, nil, p, w, id); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if !strings.Contains(id, ":") || strings.HasSuffix(id, ":") {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := s.writable(ctx, tx, p, w, id); err != nil {
			return problem(err)
		}
		return problem(s.Access.CheckVersion(ctx, tx, p, w, id, req.ExpectedVersion))
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: "automation-" + kind(id), Mutation: req.Mutation, Payload: req.Value}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		value, refs, err := s.prepare(ctx, tx, p, w, id, req.Value)
		if err != nil {
			return nil, err
		}
		if err = s.checkRefs(ctx, tx, p, w, refs); err != nil {
			return nil, err
		}
		if req.ExpectedVersion == "0" {
			if err = s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: id}, nil); err != nil {
				return nil, err
			}
		}
		return s.save(ctx, tx, w, Ref{ID: id, Version: ref.Version}, kind(id), value, refs)
	})
}
func (s *Service) save(ctx context.Context, tx pgx.Tx, w string, ref Ref, k string, value any, refs []Ref) (json.RawMessage, error) {
	if refs == nil {
		refs = []Ref{}
	}
	raw := encode(value)
	var seq int64
	if err := tx.QueryRow(ctx, "SELECT sequence+1 FROM shared_streams WHERE workspace_id=$1", w).Scan(&seq); err != nil {
		return nil, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO automation_records VALUES($1,$2,$3,$4,$5,$6,$7)`, w, ref.ID, ref.Version, k, seq, raw, encode(refs))
	return encode(Record{Ref: ref, Kind: k, Position: seq, Value: raw, Refs: refs}), err
}
func (s *Service) prepare(ctx context.Context, tx pgx.Tx, p access.Principal, w, id string, raw []byte) (any, []Ref, error) {
	refs := []Ref{}
	switch kind(id) {
	case "policy":
		var v Policy
		if decode(raw, &v) != nil || !validPolicy(v) {
			return nil, nil, ErrInvalid
		}
		for _, scope := range v.Scopes {
			refs = append(refs, Ref{ID: "scope:" + scope})
		}
		refs = append(refs, v.Context...)
		return v, refs, nil
	case "activation":
		var v Activation
		if decode(raw, &v) != nil || !fixed(v.Policy) || id != "activation:"+strings.TrimPrefix(v.Policy.ID, "policy:") {
			return nil, nil, ErrInvalid
		}
		r, err := record(ctx, tx, w, v.Policy)
		if err != nil || r.Kind != "policy" {
			return nil, nil, ErrInvalid
		}
		var policy Policy
		_ = json.Unmarshal(r.Value, &policy)
		if _, err = tx.Exec(ctx, "DELETE FROM automation_policy_targets WHERE workspace_id=$1 AND policy_id=$2", w, v.Policy.ID); err != nil {
			return nil, nil, err
		}
		if v.Enabled {
			for _, scope := range policy.Scopes {
				if _, err = tx.Exec(ctx, "INSERT INTO automation_policy_targets VALUES($1,$2,$3) ON CONFLICT DO NOTHING", w, scope, v.Policy.ID); err != nil {
					return nil, nil, err
				}
			}
		}
		refs = append(refs, v.Policy)
		refs = append(refs, r.Refs...)
		return v, refs, nil
	case "control":
		var v Control
		if decode(raw, &v) != nil || !validControl(v) || id != "control:"+v.Kind {
			return nil, nil, ErrInvalid
		}
		v.Controller = p.UserID
		if v.Budget != nil {
			n, ok := new(big.Rat).SetString(v.Budget.Amount)
			if !ok || !regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`).MatchString(v.Budget.Amount) || n.Sign() < 0 || v.Budget.Currency == "" || (v.Budget.Valuation != "reported" && v.Budget.Valuation != "estimated" && v.Budget.Valuation != "api_equivalent" && v.Budget.Valuation != "allocated") {
				return nil, nil, ErrInvalid
			}
		}
		return v, refs, nil
	case "priority":
		var v PriorityEdit
		if decode(raw, &v) != nil || !fixed(v.Target) || !fixed(v.Policy) || id != "priority:"+v.Target.ID || v.Reason == "" {
			return nil, nil, ErrInvalid
		}
		targetAction := "update"
		if p.OperationID != "" {
			targetAction = "triage_write"
		}
		if err := s.Access.CheckTx(ctx, tx, p, w, v.Target.ID, targetAction); err != nil {
			return nil, nil, err
		}
		r, err := record(ctx, tx, w, v.Policy)
		if err != nil || r.Kind != "policy" {
			return nil, nil, ErrInvalid
		}
		var policy Policy
		_ = json.Unmarshal(r.Value, &policy)
		if v.Value != nil && !hasLevel(policy, *v.Value) {
			return nil, nil, ErrInvalid
		}
		current := Priority{Target: v.Target, Policy: v.Policy, State: "unevaluated"}
		old, err := record(ctx, tx, w, Ref{ID: id, Version: "head"})
		if err == nil {
			_ = json.Unmarshal(old.Value, &current)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		current.Target = v.Target
		current.Policy = v.Policy
		current.Reason = v.Reason
		var targetVersion string
		if err := tx.QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, v.Target.ID).Scan(&targetVersion); err != nil || targetVersion != v.Target.Version {
			return nil, nil, ErrInvalid
		}
		if v.Unlock {
			current.Fixed = false
			chosen, why, _, err := s.selectPolicy(ctx, tx, w, v.Target)
			if err != nil {
				return nil, nil, err
			}
			valid := why == "" && chosen.Ref == v.Policy
			if valid && current.RecommendationTarget != nil && *current.RecommendationTarget == v.Target && current.RecommendationPolicy != nil && *current.RecommendationPolicy == v.Policy && current.State == "evaluated" {
				current.Adopted = current.Recommendation
				current.Origin = "triage"
				current.AdoptedReason = current.RecommendationReason
			}
		} else {
			if v.Value == nil {
				return nil, nil, ErrInvalid
			}
			current.Adopted = v.Value
			current.Origin = "manual"
			current.AdoptedReason = v.Reason
			current.Fixed = true
		}
		refs = append(refs, r.Refs...)
		refs = append(refs, v.Target, v.Policy)
		refs = append(refs, current.Refs...)
		return current, refs, nil
	}
	return nil, nil, ErrInvalid
}

// selectPolicy inspects at most the four Scope levels, not Context membership or
// the entire backlog. Applicable definitions remain independent and versioned.
func (s *Service) selectPolicy(ctx context.Context, q query, w string, target Ref) (Record, string, []Ref, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT payload FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3 AND kind='scope'`, w, target.ID, target.Version).Scan(&raw)
	if err != nil {
		return Record{}, "", nil, err
	}
	var scope contexts.Scope
	if json.Unmarshal(raw, &scope) != nil || scope.Kind != "issue" {
		return Record{}, "", nil, ErrInvalid
	}
	ids := []string{strings.TrimPrefix(target.ID, "scope:")}
	refs := []Ref{target}
	for parent := scope.Parent; parent != nil && len(ids) < 4; {
		var version string
		if err = q.QueryRow(ctx, `SELECT r.payload,r.revision FROM context_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.resource_id AND v.version=r.revision WHERE r.workspace_id=$1 AND r.resource_id=$2`, w, "scope:"+parent.ID).Scan(&raw, &version); err != nil {
			return Record{}, "", nil, err
		}
		ids = append(ids, parent.ID)
		refs = append(refs, Ref{ID: "scope:" + parent.ID, Version: version})
		_ = json.Unmarshal(raw, &scope)
		parent = scope.Parent
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT p.id,p.version,p.payload,p.refs,p.position,a.id,a.version FROM automation_policy_targets t
 JOIN shared_versions av ON av.workspace_id=t.workspace_id AND av.resource_id='activation:'||substring(t.policy_id from 8)
 JOIN automation_records a ON a.workspace_id=av.workspace_id AND a.id=av.resource_id AND a.version=av.version
 JOIN automation_records p ON p.workspace_id=a.workspace_id AND p.id=a.payload->'policy'->>'id' AND p.version=a.payload->'policy'->>'version'
 WHERE t.workspace_id=$1 AND t.scope_id=ANY($2) AND a.payload->>'enabled'='true'
 ORDER BY p.id`, w, ids)
	if err != nil {
		return Record{}, "", nil, err
	}
	defer rows.Close()
	baseRefs := append([]Ref{}, refs...)
	selected := Record{}
	best := 0
	conflict := false
	for rows.Next() {
		var r Record
		r.Kind = "policy"
		var rs []byte
		var activation Ref
		if err = rows.Scan(&r.ID, &r.Version, &r.Value, &rs, &r.Position, &activation.ID, &activation.Version); err != nil {
			return r, "", nil, err
		}
		_ = json.Unmarshal(rs, &r.Refs)
		var p Policy
		_ = json.Unmarshal(r.Value, &p)
		if selected.ID == "" || p.Precedence > best {
			selected = r
			best = p.Precedence
			conflict = false
			refs = append(append([]Ref{}, baseRefs...), activation, r.Ref)
			refs = append(refs, r.Refs...)
		} else if p.Precedence == best {
			conflict = true
			refs = append(refs, activation, r.Ref)
			refs = append(refs, r.Refs...)
		}
	}
	if err = rows.Err(); err != nil {
		return selected, "", nil, err
	}
	if selected.ID == "" {
		return selected, "policy_unset", refs, nil
	}
	if conflict {
		return Record{}, "policy_conflict", refs, nil
	}
	return selected, "", refs, nil
}
func (s *Service) Select(ctx context.Context, p access.Principal, w string, target Ref) (Record, string, error) {
	r, reason, refs, err := s.selectPolicy(ctx, s.Store.Database(), w, target)
	if err == nil {
		err = s.checkRefs(ctx, nil, p, w, refs)
	}
	return r, reason, err
}
func (s *Service) accepted(ctx context.Context, tx pgx.Tx, c persistence.Command, r apicontract.Reference, state json.RawMessage) error {
	if c.Kind != "context-scope" || c.Deleted {
		return nil
	}
	var rec contexts.Record
	var scope contexts.Scope
	if json.Unmarshal(state, &rec) != nil || json.Unmarshal(rec.Value, &scope) != nil || scope.Kind != "issue" {
		return nil
	}
	source := Source{Event: r.ID + "@" + r.Version, Target: Ref{ID: r.ID, Version: r.Version}}
	policy, reason, refs, err := s.selectPolicy(ctx, tx, c.WorkspaceID, source.Target)
	if err != nil {
		return err
	}
	return s.enqueue(ctx, tx, c.WorkspaceID, "triage", source, policy.Ref, reason, refs)
}

// AcceptResultTx is the contract for an owning result domain after acceptance.
// Completion/log observations do not call it. The owning write must use this tx.
func (s *Service) AcceptResultTx(ctx context.Context, tx pgx.Tx, w string, source Source, definition Ref) error {
	if !fixed(source.Target) || !fixed(definition) || source.Event == "" {
		return ErrInvalid
	}
	return s.enqueue(ctx, tx, w, "hopper", source, definition, "", []Ref{source.Target, definition})
}
func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, w, k string, source Source, definition Ref, reason string, refs []Ref) error {
	state := "waiting"
	if reason != "" {
		state = "blocked"
	}
	if refs == nil {
		refs = []Ref{}
	}
	id := rand.Text()
	tag, err := tx.Exec(ctx, `INSERT INTO automation_jobs(workspace_id,id,kind,event_id,target_id,target_version,definition_id,definition_version,state,reason,refs)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(workspace_id,kind,event_id,target_id,target_version,definition_id,definition_version) DO NOTHING`, w, id, k, source.Event, source.Target.ID, source.Target.Version, definition.ID, definition.Version, state, reason, encode(refs))
	if err == nil && tag.RowsAffected() == 1 {
		err = logJob(ctx, tx, w, id)
	}
	return err
}
