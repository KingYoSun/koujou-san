package contexts

import (
	"context"
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
	Kind  string `json:"kind"`
	ID    string `json:"id,omitempty"`
	After string `json:"after,omitempty"`
}
type CatalogPage struct {
	Items []Candidate `json:"items"`
}

func (s *Service) Catalog(ctx context.Context, p access.Principal, w string, r CatalogRequest) (apicontract.Response[CatalogPage], error) {
	if err := s.check(ctx, s.Store.Database(), p, w, "", "read"); err != nil {
		return apicontract.Response[CatalogPage]{}, err
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
