package searchindex

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/query"
	indexapi "github.com/blevesearch/bleve_index_api"
)

func resultMeta(w, position, state, reason string) apicontract.Meta {
	var marker *string
	if position != "" {
		marker = &position
	}
	return apicontract.Meta{WorkspaceID: w, Position: marker, Completeness: apicontract.Completeness{State: state, Reason: reason}}
}
func failed(w, code, reason string) apicontract.Response[Result] {
	return apicontract.Response[Result]{Error: &apicontract.Problem{Code: code}, Meta: resultMeta(w, "", "pending", reason)}
}
func readSource(i bleve.Index, id string) (source, error) {
	var value source
	doc, err := i.Document(id)
	if err != nil {
		return value, err
	}
	if doc == nil {
		return value, errors.New("missing index document")
	}
	var raw []byte
	doc.VisitFields(func(f indexapi.Field) {
		if f.Name() == "source" {
			raw = f.Value()
		}
	})
	err = json.Unmarshal(raw, &value)
	return value, err
}
func permissionError(err error) bool {
	return errors.Is(err, access.ErrForbidden) || err.Error() == "forbidden"
}

// Query filters current authorization before Bleve counts or paginates matches.
// Its cursor is encrypted so raw engine sort keys cannot disclose denied IDs.
func (s *Service) Query(ctx context.Context, p access.Principal, w string, r Request) (apicontract.Response[Result], error) {
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return failed(w, "forbidden", "not_accessed"), err
	}
	q, err := makeQuery(w, r)
	if err != nil {
		return failed(w, "invalid_request", "invalid_search"), err
	}
	current, _, err := head(ctx, s.Store.Database(), w)
	if err != nil {
		return failed(w, "unavailable", "storage_unavailable"), err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.index == nil {
		return failed(w, "unavailable", "search_unavailable"), nil
	}
	cp, err := readCheckpoint(s.index, w)
	if err != nil {
		return failed(w, "unavailable", "search_unavailable"), err
	}
	request := r
	request.Cursor = ""
	signature := hash(request)
	var after []string
	if r.Cursor != "" {
		c, err := s.open(r.Cursor)
		if err != nil || c.User != p.UserID || c.Workspace != w || c.Query != signature || c.Generation != s.generation || c.Position != cp.Position {
			return failed(w, "invalid_request", "invalid_cursor"), nil
		}
		after = c.After
	}
	stale := false
	q = query.NewCustomFilterQueryWithFilter(q, func(ctx context.Context, hit *search.DocumentMatch) (bool, error) {
		src, err := readSource(s.index, hit.ID)
		if err != nil {
			return false, err
		}
		for _, ref := range src.Dependencies {
			if _, err = s.Contexts.Read(ctx, p, w, ref); err != nil {
				if permissionError(err) {
					return false, nil
				}
				if err.Error() == "missing_reference" {
					stale = true
					return false, nil
				}
				return false, err
			}
		}
		var version string
		var deleted bool
		if err = s.Store.Database().QueryRow(ctx, "SELECT version,deleted FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, "context:"+src.Ref.ID).Scan(&version, &deleted); err != nil {
			return false, err
		}
		if deleted {
			return false, nil
		}
		if len(r.Revisions) == 0 && version != src.Ref.Revision {
			stale = true
			return false, nil
		}
		return true, nil
	}, nil, nil)
	searchRequest := bleve.NewSearchRequestOptions(q, r.Limit+1, 0, false)
	searchRequest.SortBy([]string{"-_score", "_id"})
	if len(after) > 0 {
		searchRequest.SetSearchAfter(after)
	}
	found, err := s.index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return failed(w, "unavailable", "search_unavailable"), err
	}
	items := []contexts.Record{}
	hits := found.Hits
	more := len(hits) > r.Limit
	if more {
		hits = hits[:r.Limit]
	}
	for _, hit := range hits {
		src, err := readSource(s.index, hit.ID)
		if err != nil {
			return failed(w, "unavailable", "search_unavailable"), err
		}
		// Recheck after search as well: membership or a target grant may have changed
		// while the engine was evaluating other matches.
		allowed := true
		for _, ref := range src.Dependencies {
			if _, err = s.Contexts.Read(ctx, p, w, ref); err != nil {
				if permissionError(err) {
					allowed = false
					break
				}
				return failed(w, "unavailable", "storage_unavailable"), err
			}
		}
		if allowed {
			rec, err := s.Contexts.Read(ctx, p, w, src.Ref)
			if err != nil {
				return failed(w, "unavailable", "storage_unavailable"), err
			}
			items = append(items, rec)
		}
	}
	// A specifically requested SoT revision may not be present in a damaged or
	// delayed index. Do not turn that known missing projection into complete zero.
	if len(r.Revisions) > 0 {
		rows, err := s.Store.Database().Query(ctx, `SELECT r.kind,r.resource_id,r.revision FROM context_records r JOIN shared_versions v USING(workspace_id,resource_id) WHERE r.workspace_id=$1 AND NOT v.deleted AND r.revision=ANY($2) AND (cardinality($3::text[])=0 OR r.resource_id=ANY($3)) AND r.kind IN ('sentence','paragraph','section','document')`, w, r.Revisions, prefixIDs(r.IDs))
		if err != nil {
			return failed(w, "unavailable", "storage_unavailable"), err
		}
		for rows.Next() {
			var ref contexts.Ref
			var id string
			if err = rows.Scan(&ref.Kind, &id, &ref.Revision); err != nil {
				break
			}
			ref.ID = strings.TrimPrefix(id, "context:")
			if _, readErr := s.Contexts.Read(ctx, p, w, ref); readErr != nil {
				if permissionError(readErr) {
					continue
				}
				err = readErr
				break
			}
			doc, readErr := s.index.Document(docID(w, ref))
			if readErr != nil {
				err = readErr
				break
			}
			if doc == nil {
				stale = true
			}
		}
		rows.Close()
		if err != nil {
			return failed(w, "unavailable", "search_unavailable"), err
		}
		if err = rows.Err(); err != nil {
			return failed(w, "unavailable", "storage_unavailable"), err
		}
	}
	state, reason := "complete", ""
	if err = s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return failed(w, "forbidden", "not_accessed"), err
	}
	if more {
		state, reason = "partial", "limit_reached"
	}
	if stale {
		state, reason = "pending", "required_version_pending"
	}
	if cp.Position != current.Position {
		state, reason = "pending", "projection_pending"
	}
	result := apicontract.Response[Result]{Data: &Result{Items: items, Count: len(items), Evidence: contexts.CandidateEvidence{Engine: "bleve", Position: cp.Position, RequiredPosition: current.Position, State: state, Reason: reason}}, Meta: resultMeta(w, cp.Position, state, reason)}
	if more && len(hits) > 0 {
		last := hits[len(hits)-1]
		after := append([]string{}, last.Sort...)
		// Bleve's in-process score sort key is a sentinel; SearchAfter requires
		// the actual numeric score, just as the JSON pagination API does.
		after[0] = strconv.FormatFloat(last.Score, 'g', -1, 64)
		next := s.seal(cursor{User: p.UserID, Workspace: w, Query: signature, Generation: s.generation, Position: cp.Position, After: after})
		result.Meta.NextCursor = &next
	}
	return result, nil
}
func prefixIDs(ids []string) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = "context:" + id
	}
	return result
}
