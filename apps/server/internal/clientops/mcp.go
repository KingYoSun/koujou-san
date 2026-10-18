package clientops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/jackc/pgx/v5"
)

func (s *Service) MCPBinding(ctx context.Context, q query, p access.Principal, w string) (Operation, bool, error) {
	var op Operation
	var b []byte
	err := q.QueryRow(ctx, `SELECT r.payload FROM client_operation_heads c JOIN shared_versions h ON h.workspace_id=c.workspace_id AND h.resource_id=c.id JOIN client_operation_records r ON r.workspace_id=h.workspace_id AND r.id=h.resource_id AND r.version=h.version WHERE c.workspace_id=$1 AND c.read_operation=$2 AND c.kind='operation'`, w, p.OperationID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return op, false, nil
	}
	if err != nil {
		return op, true, err
	}
	if decode(b, &op) != nil || op.User != p.UserID {
		return op, true, access.ErrForbidden
	}
	return op, true, nil
}
func (s *Service) ChargeMCP(ctx context.Context, p access.Principal, w, client string) error {
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		op, found, err := s.MCPBinding(ctx, tx, p, w)
		if err != nil || !found {
			return err
		}
		if op.State != "ready" || op.Client != client || client == "" {
			return access.ErrForbidden
		}
		policy, err := s.currentPolicy(ctx, tx, access.Principal{UserID: p.UserID}, w, op)
		if err != nil {
			return access.ErrForbidden
		}
		var count int
		var deadline *time.Time
		if err = tx.QueryRow(ctx, `SELECT mcp,deadline FROM client_operation_heads WHERE workspace_id=$1 AND read_operation=$2 FOR UPDATE`, w, p.OperationID).Scan(&count, &deadline); err != nil {
			return err
		}
		if count >= policy.Limits.MCP || (deadline != nil && !time.Now().Before(*deadline)) {
			return access.ErrForbidden
		}
		_, err = tx.Exec(ctx, `UPDATE client_operation_heads SET mcp=mcp+1 WHERE workspace_id=$1 AND read_operation=$2`, w, p.OperationID)
		return err
	})
}
func (s *Service) CheckMCPPolicy(ctx context.Context, p access.Principal, w string) error {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found {
		return err
	}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), access.Principal{UserID: p.UserID}, w, op)
	if err == nil && policy.Generation != nil {
		_, deps, sourceErr := s.generationSource(ctx, access.Principal{UserID: p.UserID}, w, policy.Generation)
		if sourceErr != nil {
			return sourceErr
		}
		err = s.checkRefs(ctx, s.Store.Database(), access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}, w, append(deps, generationRefs(policy.Generation)...))
	}
	return err
}
func (s *Service) CandidatePrincipal(ctx context.Context, p access.Principal, w string) (access.Principal, []contexts.Identity, bool, error) {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found {
		return p, nil, found, err
	}
	owner := access.Principal{UserID: p.UserID}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), owner, w, op)
	if err != nil {
		return p, nil, true, err
	}
	scopes := []contexts.Identity{}
	for _, id := range policy.Scopes {
		r, err := s.Contexts.Read(ctx, owner, w, contexts.Ref{Kind: "scope", ID: id, Revision: "head"})
		if err != nil {
			return p, nil, true, err
		}
		var scope contexts.Scope
		if json.Unmarshal(r.Value, &scope) != nil {
			return p, nil, true, ErrInvalid
		}
		scopes = append(scopes, contexts.Identity{Kind: scope.Kind, ID: id})
	}
	return access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}, scopes, true, nil
}

// resolveSelection builds internally, then verifies only the delivered placements.
// Excluded structures never enter model messages or the compact MCP result.
func (s *Service) resolveSelection(ctx context.Context, tx pgx.Tx, p access.Principal, w string, op Operation, input contexts.Selection) (contexts.Resolution, error) {
	owner := access.Principal{UserID: p.UserID}
	reader := access.Principal{UserID: p.UserID, OperationID: op.ReadOperation}
	candidate := access.Principal{UserID: p.UserID, OperationID: op.CandidateOperation}
	for _, r := range input.Include {
		if err := s.Access.CheckVersion(ctx, tx, reader, w, resource(r), r.Revision); err != nil {
			return contexts.Resolution{}, err
		}
	}
	resolved := s.Contexts.InTransaction(tx).Preview(ctx, owner, w, contexts.ResolveRequest{Selection: input})
	if !resolved.Ready {
		return resolved, nil
	}
	refs := []Ref{}
	for _, placement := range resolved.Manifest.Placements {
		refs = append(refs, Ref{ID: resource(placement.Ref), Version: placement.Ref.Revision})
	}
	if err := s.checkRefs(ctx, tx, candidate, w, refs); err != nil {
		return contexts.Resolution{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO access_operation_refs SELECT $1,$2,r.id,r.version FROM jsonb_to_recordset($3::jsonb) r(id text,version text) ON CONFLICT DO NOTHING`, w, op.ReadOperation, raw(refs)); err != nil {
		return contexts.Resolution{}, err
	}
	return resolved, nil
}
func (s *Service) BeforeGet(ctx context.Context, p access.Principal, w string, ref contexts.Ref) error {
	return pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		op, found, err := s.MCPBinding(ctx, tx, p, w)
		if err != nil || !found {
			return err
		}
		policy, err := s.currentPolicy(ctx, tx, access.Principal{UserID: p.UserID}, w, op)
		if err != nil {
			return err
		}
		resolved, err := s.resolveSelection(ctx, tx, p, w, op, contexts.Selection{Mode: "select", Include: []contexts.Ref{ref}, Exclude: policy.Fixed.Exclude})
		if err != nil {
			return err
		}
		if !resolved.Ready || len(resolved.Manifest.Excluded) > 0 {
			return access.ErrForbidden
		}
		return nil
	})
}

type ModelResolution struct {
	Ready      bool                 `json:"ready"`
	State      string               `json:"state"`
	Digest     string               `json:"digest"`
	Placements []contexts.Placement `json:"placements"`
	Missing    []string             `json:"missing"`
}

func (s *Service) mcpSources(ctx context.Context, q query, w, operation string, since *time.Time, complete bool, contextOnly ...bool) ([]Ref, error) {
	onlyContext := len(contextOnly) > 0 && contextOnly[0]
	rows, err := q.Query(ctx, `SELECT DISTINCT r.id,r.version FROM access_operation_events e CROSS JOIN LATERAL jsonb_to_recordset(CASE WHEN jsonb_typeof(e.result->'refs')='array' THEN e.result->'refs' ELSE '[]'::jsonb END) r(id text,version text) WHERE e.workspace_id=$1 AND e.operation_id=$2 AND e.result->>'outcome'='returned' AND ($3::timestamptz IS NULL OR e.created_at>=$3) AND (NOT $5 OR e.method IN ('context_search','context_graph','context_get','context_resolve')) AND (NOT $4 OR (e.result#>>'{meta,completeness,state}'='complete' OR (e.result#>>'{meta,completeness,state}'='partial' AND e.result#>>'{meta,completeness,reason}'='limit_reached' AND COALESCE(e.result#>>'{meta,next_cursor}','')<>'')))`, w, operation, since, complete, onlyContext)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []Ref{}
	for rows.Next() {
		var r Ref
		if err = rows.Scan(&r.ID, &r.Version); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
func (s *Service) candidateEvidence(ctx context.Context, q query, w string, op Operation, p Policy, proposal Proposal) error {
	var incomplete bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT DISTINCT ON(method,result->>'query_key') result FROM access_operation_events WHERE workspace_id=$1 AND operation_id=$2 AND ($3::timestamptz IS NULL OR created_at>=$3) AND method IN ('context_search','context_graph','context_resolve') AND result->>'outcome'='returned' AND result->>'query_key'<>'' ORDER BY method,result->>'query_key',created_at DESC,id DESC) latest WHERE NOT (result#>>'{meta,completeness,state}'='complete' OR (result#>>'{meta,completeness,state}'='partial' AND result#>>'{meta,completeness,reason}'='limit_reached' AND COALESCE(result#>>'{meta,next_cursor}','')<>'')))`, w, op.ReadOperation, op.EvidenceSince).Scan(&incomplete); err != nil {
		return err
	}
	if incomplete {
		return ErrState
	}
	known, err := s.mcpSources(ctx, q, w, op.ReadOperation, op.EvidenceSince, true)
	if err != nil {
		return err
	}
	seen := map[Ref]bool{}
	for _, r := range known {
		seen[r] = true
	}
	for _, r := range p.Fixed.Include {
		seen[Ref{ID: resource(r), Version: r.Revision}] = true
	}
	for _, item := range proposal.Items {
		if !seen[Ref{ID: resource(item.Ref), Version: item.Ref.Revision}] {
			return ErrState
		}
	}
	return nil
}

func (s *Service) PreviewForModel(ctx context.Context, p access.Principal, w string, input contexts.Selection) (ModelResolution, []contexts.Ref, bool, error) {
	var out ModelResolution
	refs := []contexts.Ref{}
	found := false
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		op, ok, err := s.MCPBinding(ctx, tx, p, w)
		found = ok
		if err != nil || !ok {
			return err
		}
		policy, err := s.currentPolicy(ctx, tx, access.Principal{UserID: p.UserID}, w, op)
		if err != nil {
			return err
		}
		if len(input.Direct) > 0 || len(input.External) > 0 {
			return access.ErrForbidden
		}
		if policy.Fixed.Mode == "none" {
			if input.Mode != "none" || len(input.Include) > 0 || len(input.Exclude) > 0 {
				return access.ErrForbidden
			}
		} else {
			input.Exclude = append(input.Exclude, policy.Fixed.Exclude...)
		}
		resolved, err := s.resolveSelection(ctx, tx, p, w, op, input)
		if err != nil {
			return err
		}
		out.Ready = resolved.Ready
		out.Missing = []string{}
		for _, gap := range resolved.Missing {
			out.Missing = append(out.Missing, gap.Code)
		}
		if resolved.Manifest != nil {
			out.State = resolved.Manifest.State
			out.Digest = resolved.Manifest.Digest
			out.Placements = resolved.Manifest.Placements
			for _, v := range out.Placements {
				refs = append(refs, v.Ref)
			}
		}
		return nil
	})
	return out, refs, found, err
}

func (s *Service) CheckReadTool(ctx context.Context, p access.Principal, w, name string) error {
	op, found, err := s.MCPBinding(ctx, s.Store.Database(), p, w)
	if err != nil || !found {
		return err
	}
	if !ReadTool(name, op.Purpose) {
		return access.ErrForbidden
	}
	policy, err := s.currentPolicy(ctx, s.Store.Database(), access.Principal{UserID: p.UserID}, w, op)
	if err != nil {
		return err
	}
	if policy.Fixed.Mode == "none" && strings.HasPrefix(name, "context_") {
		return access.ErrForbidden
	}
	return nil
}
