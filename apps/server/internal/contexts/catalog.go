package contexts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

// Candidate contains bounded display metadata. Fixed content is fetched only
// when the user opens a version, rather than as part of every candidate page.
type Candidate struct {
	Ref       Ref       `json:"ref"`
	Name      string    `json:"name"`
	ScopeKind string    `json:"scope_kind,omitempty"`
	Parent    *Identity `json:"parent,omitempty"`
}

func CandidateOf(record Record) Candidate {
	var value struct {
		Name, Title string
		Text        *string
	}
	_ = json.Unmarshal(record.Value, &value)
	name := value.Title
	if name == "" {
		name = value.Name
	}
	if name == "" && value.Text != nil {
		name = *value.Text
	}
	chars := []rune(name)
	if len(chars) > 160 {
		name = string(chars[:160])
	}
	return Candidate{Ref: record.Ref, Name: name}
}

type CatalogRequest struct {
	Kind      string `json:"kind"`
	ID        string `json:"id,omitempty"`
	After     string `json:"after,omitempty"`
	ScopeKind string `json:"scope_kind,omitempty"`
	Search    string `json:"search,omitempty"`
}
type CatalogPage struct {
	Items []Candidate `json:"items"`
}

func (s *Service) Catalog(ctx context.Context, p access.Principal, w string, r CatalogRequest) (apicontract.Response[CatalogPage], error) {
	if err := s.check(ctx, s.Store.Database(), p, w, "", "read"); err != nil {
		return apicontract.Response[CatalogPage]{}, err
	}
	if r.ScopeKind != "" || r.Search != "" {
		if r.Kind != "scope" || r.ID != "" || scopeLevel(r.ScopeKind) == 0 {
			return apicontract.Response[CatalogPage]{}, invalid("invalid_request", nil)
		}
		return s.scopeCatalog(ctx, p, w, r)
	}
	history := r.ID != ""
	if (!history && r.Kind != "scope") || (history && unitLevel(r.Kind) == 0 && r.Kind != "scope" && r.Kind != "membership") {
		return apicontract.Response[CatalogPage]{}, invalid("invalid_request", nil)
	}
	if history {
		if err := s.check(ctx, s.Store.Database(), p, w, resource(Identity{r.Kind, r.ID}), "get"); err != nil {
			return apicontract.Response[CatalogPage]{}, err
		}
	}
	before := int64(9223372036854775807)
	if history && r.After != "" {
		if err := s.Store.Database().QueryRow(ctx, "SELECT stream_sequence FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3", w, resource(Identity{r.Kind, r.ID}), r.After).Scan(&before); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apicontract.Response[CatalogPage]{}, invalid("invalid_cursor", nil)
			}
			return apicontract.Response[CatalogPage]{}, err
		}
	}
	page := CatalogPage{Items: []Candidate{}}
	after := r.After
	var next *string
	for len(page.Items) <= 100 {
		var rows pgx.Rows
		var err error
		const fields = "r.kind,r.resource_id,r.revision,left(coalesce(r.payload->>'title',r.payload->>'name',r.payload->>'text',''),160),coalesce(r.payload->>'kind',''),r.payload->'parent',r.stream_sequence"
		if history {
			rows, err = s.Store.Database().Query(ctx, "SELECT "+fields+" FROM context_records r WHERE workspace_id=$1 AND resource_id=$2 AND stream_sequence<$3 ORDER BY stream_sequence DESC LIMIT 101", w, resource(Identity{r.Kind, r.ID}), before)
		} else {
			rows, err = s.Store.Database().Query(ctx, "SELECT "+fields+" FROM shared_versions v JOIN context_records r ON r.workspace_id=v.workspace_id AND r.resource_id=v.resource_id AND r.revision=v.version WHERE v.workspace_id=$1 AND v.resource_id LIKE 'scope:%' AND v.resource_id>$2 AND NOT v.deleted ORDER BY v.resource_id LIMIT 101", w, "scope:"+after)
		}
		if err != nil {
			return apicontract.Response[CatalogPage]{}, err
		}
		type entry struct {
			candidate Candidate
			resource  string
			sequence  int64
		}
		entries := []entry{}
		for rows.Next() {
			var e entry
			var parent []byte
			err = rows.Scan(&e.candidate.Ref.Kind, &e.resource, &e.candidate.Ref.Revision, &e.candidate.Name, &e.candidate.ScopeKind, &parent, &e.sequence)
			if err != nil {
				break
			}
			e.candidate.Ref.ID = strings.SplitN(e.resource, ":", 2)[1]
			if string(parent) != "null" && len(parent) > 0 {
				if err = json.Unmarshal(parent, &e.candidate.Parent); err != nil {
					break
				}
			}
			if e.candidate.Ref.Kind != "scope" {
				e.candidate.ScopeKind = ""
			}
			entries = append(entries, e)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return apicontract.Response[CatalogPage]{}, err
		}
		for _, e := range entries {
			after = e.candidate.Ref.ID
			before = e.sequence
			if err = s.check(ctx, s.Store.Database(), p, w, e.resource, "get"); err != nil {
				if err.Error() == "forbidden" {
					continue
				}
				return apicontract.Response[CatalogPage]{}, err
			}
			if err = s.Access.CheckVersion(ctx, nil, p, w, e.resource, e.candidate.Ref.Revision); err != nil {
				if err.Error() == "forbidden" {
					continue
				}
				return apicontract.Response[CatalogPage]{}, err
			}
			page.Items = append(page.Items, e.candidate)
			if len(page.Items) > 100 {
				last := page.Items[99].Ref
				value := last.ID
				if history {
					value = last.Revision
				}
				next = &value
				page.Items = page.Items[:100]
				break
			}
		}
		if next != nil || len(entries) < 101 {
			break
		}
	}
	return s.catalogResult(ctx, w, page, next)
}

func (s *Service) catalogResult(ctx context.Context, w string, page CatalogPage, next *string) (apicontract.Response[CatalogPage], error) {
	var position string
	if err := s.Store.Database().QueryRow(ctx, "SELECT position FROM shared_streams WHERE workspace_id=$1", w).Scan(&position); err != nil {
		return apicontract.Response[CatalogPage]{}, err
	}
	meta := metadata(w, &position, "complete", "")
	meta.NextCursor = next
	if next != nil {
		meta.Completeness = apicontract.Completeness{State: "partial", Reason: "page_remaining"}
	}
	return apicontract.Response[CatalogPage]{Data: &page, Meta: meta}, nil
}

func (s *Service) scopeCatalog(ctx context.Context, p access.Principal, w string, r CatalogRequest) (apicontract.Response[CatalogPage], error) {
	type cursor struct{ Kind, Search, Name, ID string }
	c := cursor{Kind: r.ScopeKind, Search: r.Search}
	if r.After != "" {
		raw, err := base64.RawURLEncoding.DecodeString(r.After)
		if err != nil || json.Unmarshal(raw, &c) != nil || c.Kind != r.ScopeKind || c.Search != r.Search {
			return apicontract.Response[CatalogPage]{}, invalid("invalid_cursor", nil)
		}
	}
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(r.Search) + "%"
	rows, err := s.Store.Database().Query(ctx, `SELECT r.resource_id,r.revision,left(r.payload->>'name',160),r.payload->'parent',c.sort_name
 FROM context_scope_catalog c JOIN context_records r ON r.workspace_id=c.workspace_id AND r.resource_id=c.resource_id AND r.revision=c.revision
 WHERE c.workspace_id=$1 AND c.scope_kind=$4 AND c.sort_name LIKE lower($5) AND (c.sort_name,c.resource_id)>($6,$7)
 AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id',r.resource_id,'version',r.revision))`)+`
 ORDER BY c.sort_name,c.resource_id LIMIT 101`, w, p.UserID, p.OperationID, r.ScopeKind, pattern, c.Name, c.ID)
	if err != nil {
		return apicontract.Response[CatalogPage]{}, err
	}
	defer rows.Close()
	page := CatalogPage{Items: []Candidate{}}
	var next *string
	last := c
	for rows.Next() {
		var item Candidate
		var parent []byte
		var id, name string
		if err = rows.Scan(&id, &item.Ref.Revision, &item.Name, &parent, &name); err != nil {
			return apicontract.Response[CatalogPage]{}, err
		}
		if len(page.Items) == 100 {
			raw, _ := json.Marshal(last)
			value := base64.RawURLEncoding.EncodeToString(raw)
			next = &value
			break
		}
		item.Ref.Kind = "scope"
		item.Ref.ID = strings.TrimPrefix(id, "scope:")
		item.ScopeKind = r.ScopeKind
		if string(parent) != "null" && len(parent) > 0 {
			if err = json.Unmarshal(parent, &item.Parent); err != nil {
				return apicontract.Response[CatalogPage]{}, err
			}
		}
		page.Items = append(page.Items, item)
		last.Name = name
		last.ID = id
	}
	if err = rows.Err(); err != nil {
		return apicontract.Response[CatalogPage]{}, err
	}
	rows.Close()
	return s.catalogResult(ctx, w, page, next)
}
func (s *Service) catalogHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request CatalogRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		sendError(w, workspace, invalid("invalid_request", nil))
		return
	}
	result, err := s.Catalog(r.Context(), p, workspace, request)
	if err != nil {
		sendError(w, workspace, err)
		return
	}
	writeJSON(w, 200, result)
}
