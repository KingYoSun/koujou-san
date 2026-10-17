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
	subject, err := s.Access.CursorSubject(ctx, p, w)
	if err != nil {
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
		if err != nil || c.User != subject || c.Workspace != w || c.Query != signature || c.Generation != s.generation {
			return failed(w, "invalid_request", "invalid_cursor"), nil
		}
		if c.Position != cp.Position {
			var previous int64
			if err = s.Store.Database().QueryRow(ctx, `SELECT sequence FROM shared_changes WHERE workspace_id=$1 AND position=$2`, w, c.Position).Scan(&previous); err != nil {
				return failed(w, "invalid_request", "invalid_cursor"), nil
			}
			changed, err := s.Store.ProjectionChanged(ctx, w, previous, cp.Sequence)
			if err != nil {
				return failed(w, "unavailable", "storage_unavailable"), err
			}
			if changed {
				return failed(w, "invalid_request", "invalid_cursor"), nil
			}
		}
		after = c.After
	}
	stale := false
	// Read ordered engine pages until one authorized page and its look-ahead
	// are known. A broad query must not authorize every matching Context before
	// returning its first page; raw engine counts/cursors never leave this loop.
	items := []contexts.Record{}
	hits := []*search.DocumentMatch{}
	scanAfter := after
	for len(items) <= r.Limit {
		request := bleve.NewSearchRequestOptions(q, pageSize+1, 0, false)
		request.SortBy([]string{"-_score", "_id"})
		if len(scanAfter) > 0 {
			request.SetSearchAfter(scanAfter)
		}
		found, err := s.index.SearchInContext(ctx, request)
		if err != nil {
			return failed(w, "unavailable", "search_unavailable"), err
		}
		for _, hit := range found.Hits {
			scanAfter = hitPosition(hit)
			src, err := readSource(s.index, hit.ID)
			if err != nil {
				return failed(w, "unavailable", "search_unavailable"), err
			}
			allowed := true
			var record contexts.Record
			for _, ref := range src.Dependencies {
				rec, err := s.Contexts.Read(ctx, p, w, ref)
				if err != nil {
					if permissionError(err) {
						allowed = false
						break
					}
					if err.Error() == "missing_reference" {
						stale = true
						allowed = false
						break
					}
					return failed(w, "unavailable", "storage_unavailable"), err
				}
				if ref == src.Ref {
					record = rec
				}
			}
			if !allowed {
				continue
			}
			var version string
			var deleted bool
			if err = s.Store.Database().QueryRow(ctx, "SELECT version,deleted FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, "context:"+src.Ref.ID).Scan(&version, &deleted); err != nil {
				return failed(w, "unavailable", "storage_unavailable"), err
			}
			if deleted {
				continue
			}
			if len(r.Revisions) == 0 && len(r.Refs) == 0 && version != src.Ref.Revision {
				stale = true
				continue
			}
			items = append(items, record)
			hits = append(hits, hit)
			if len(items) > r.Limit {
				break
			}
		}
		if len(found.Hits) < pageSize+1 {
			break
		}
	}
	more := len(items) > r.Limit
	if more {
		items = items[:r.Limit]
		hits = hits[:r.Limit]
	}
	// Recheck the returned page after evaluating later candidates. Classification
	// and ACL changes during a query cannot leave previously allowed bodies in it.
	fresh := []contexts.Record{}
	for i, record := range items {
		src, err := readSource(s.index, hits[i].ID)
		if err != nil {
			return failed(w, "unavailable", "search_unavailable"), err
		}
		allowed := true
		for _, ref := range src.Dependencies {
			if ref == record.Ref {
				continue
			}
			if _, err = s.Contexts.Read(ctx, p, w, ref); err != nil {
				if permissionError(err) {
					allowed = false
					break
				}
				return failed(w, "unavailable", "storage_unavailable"), err
			}
		}
		if !allowed {
			continue
		}
		rec, err := s.Contexts.Read(ctx, p, w, record.Ref)
		if err != nil {
			if permissionError(err) {
				continue
			}
			return failed(w, "unavailable", "storage_unavailable"), err
		}
		fresh = append(fresh, rec)
	}
	items = fresh
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
	for _, ref := range r.Refs {
		if _, err := s.Contexts.Read(ctx, p, w, ref); err != nil {
			if permissionError(err) {
				continue
			}
			return failed(w, "unavailable", "required_version_pending"), err
		}
		doc, err := s.index.Document(docID(w, ref))
		if err != nil {
			return failed(w, "unavailable", "search_unavailable"), err
		}
		if doc == nil {
			stale = true
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
	changed, err := s.Store.ProjectionChanged(ctx, w, cp.Sequence, current.Sequence)
	if err != nil {
		return failed(w, "unavailable", "storage_unavailable"), err
	}
	if changed {
		state, reason = "pending", "projection_pending"
	}
	result := apicontract.Response[Result]{Data: &Result{Items: items, Count: len(items), Evidence: contexts.CandidateEvidence{Engine: "bleve", Position: cp.Position, RequiredPosition: current.Position, State: state, Reason: reason}}, Meta: resultMeta(w, cp.Position, state, reason)}
	if more && len(hits) > 0 {
		last := hits[len(hits)-1]
		after := hitPosition(last)
		next := s.seal(cursor{User: subject, Workspace: w, Query: signature, Generation: s.generation, Position: cp.Position, After: after})
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

// Bleve's in-process score key is a sentinel; SearchAfter needs the score.
func hitPosition(hit *search.DocumentMatch) []string {
	after := append([]string{}, hit.Sort...)
	after[0] = strconv.FormatFloat(hit.Score, 'g', -1, 64)
	return after
}
