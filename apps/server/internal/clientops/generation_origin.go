package clientops

import (
	"context"
	"encoding/json"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type GenerationOrigin struct {
	Manual    bool             `json:"manual"`
	Operation Ref              `json:"operation"`
	Policy    Ref              `json:"policy"`
	Source    GenerationSource `json:"source"`
	Item      GeneratedContext `json:"item"`
	Edited    bool             `json:"edited"`
}

func (s *Service) GenerationOrigin(ctx context.Context, p access.Principal, w, id, version, operation string) (*GenerationOrigin, error) {
	if err := s.Access.Check(ctx, p, w, "context:"+id, "get"); err != nil {
		return nil, err
	}
	if version == "" || version == "head" {
		return nil, ErrInvalid
	}
	var exists bool
	if err := s.Store.Database().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3)`, w, "context:"+id, version).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrState
	}
	var b, dependencies []byte
	err := s.Store.Database().QueryRow(ctx, `SELECT payload,refs FROM client_context_origins WHERE workspace_id=$1 AND context_id=$2 AND version=$3`, w, id, version).Scan(&b, &dependencies)
	if err == pgx.ErrNoRows && operation != "" {
		err = s.Store.Database().QueryRow(ctx, `SELECT payload,refs FROM client_context_origins WHERE workspace_id=$1 AND operation_id=$2 AND context_id=$3 ORDER BY version LIMIT 1`, w, operation, id).Scan(&b, &dependencies)
	}
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var refs []Ref
	json.Unmarshal(dependencies, &refs)
	if err = s.checkRefs(ctx, s.Store.Database(), p, w, refs); err != nil {
		return nil, err
	}
	var origin GenerationOrigin
	if decode(b, &origin) != nil {
		return nil, ErrState
	}
	origin.Edited = origin.Item.Ref.Revision != version
	return &origin, nil
}

type GenerationResultPage struct {
	Operation   Ref                `json:"operation"`
	Source      GenerationSource   `json:"source"`
	Items       []GeneratedContext `json:"items"`
	Next        string             `json:"next"`
	Memberships int                `json:"memberships"`
}

func (s *Service) GenerationResultPage(ctx context.Context, p access.Principal, w string, ref Ref, after string) (apicontract.Response[GenerationResultPage], error) {
	empty := apicontract.Response[GenerationResultPage]{}
	if !pinned(ref) {
		return empty, ErrInvalid
	}
	if err := s.Access.Check(ctx, p, w, ref.ID, "get"); err != nil {
		return empty, err
	}
	var operation, source []byte
	var count int
	err := s.Store.Database().QueryRow(ctx, `SELECT payload->'operation',payload->'source',jsonb_array_length(payload->'memberships') FROM client_operation_records WHERE workspace_id=$1 AND id=$2 AND version=$3 AND kind='context-result'`, w, ref.ID, ref.Version).Scan(&operation, &source, &count)
	if err != nil {
		return empty, err
	}
	page := GenerationResultPage{Items: []GeneratedContext{}, Memberships: count}
	json.Unmarshal(operation, &page.Operation)
	json.Unmarshal(source, &page.Source)
	if _, _, err = s.generationSource(ctx, p, w, &GenerationPolicy{Source: page.Source, Fixed: []ContextLock{}}); err != nil {
		return empty, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT payload FROM client_context_origins WHERE workspace_id=$1 AND operation_id=$5 AND context_id>$4 AND `+access.ReadFilterSQL("refs")+` ORDER BY context_id,version LIMIT 51`, w, p.UserID, p.OperationID, after, page.Operation.ID)
	// Keep the common authorization placeholders ($1 workspace/$2 user/$3 operation)
	// separate from the indexed generation operation ID.
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return empty, err
		}
		var origin GenerationOrigin
		json.Unmarshal(b, &origin)
		page.Items = append(page.Items, origin.Item)
	}
	if err = rows.Err(); err != nil {
		return empty, err
	}
	meta := apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.Next = page.Items[49].Ref.ID
		meta.NextCursor = &page.Next
		meta.Completeness = apicontract.Completeness{State: "partial", Reason: "limit_reached"}
	}
	return apicontract.Response[GenerationResultPage]{Data: &page, Meta: meta}, nil
}
