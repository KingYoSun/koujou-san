// Package sharedsync reads authenticated pages and never treats a notification
// as committed content. Cursors identify ranges; they confer no access rights.
package sharedsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

const pageSize = 100

type Service struct {
	Store    *persistence.Store
	Access   *access.Service
	Contexts *contexts.Service
	key      []byte
}

func New(store *persistence.Store, a *access.Service, c *contexts.Service, key []byte) *Service {
	return &Service{store, a, c, key}
}

type Request struct {
	Mode     string              `json:"mode"`
	Cursor   string              `json:"cursor,omitempty"`
	Position string              `json:"position,omitempty"`
	Through  string              `json:"through,omitempty"`
	Known    []contexts.Identity `json:"known,omitempty"`
}
type Item struct {
	Record   contexts.Record `json:"record"`
	Editable bool            `json:"editable"`
}
type Frame struct {
	Items       []Item              `json:"items"`
	Removed     []contexts.Identity `json:"removed"`
	Offered     []contexts.Identity `json:"offered"`
	Permissions []Permission        `json:"permissions"`
}
type Permission struct {
	Identity contexts.Identity `json:"identity"`
	Editable bool              `json:"editable"`
}
type cursor struct {
	Mode, Workspace, User, Position, After string
	Sequence                               int64
}

var errCursor = errors.New("invalid_cursor")
var errHistory = errors.New("history_unavailable")
var errInput = errors.New("invalid_request")

func encode(value any) []byte { raw, _ := json.Marshal(value); return raw }
func (s *Service) sign(raw string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("sync-cursor\x00" + raw))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) pack(c cursor) string {
	raw := base64.RawURLEncoding.EncodeToString(encode(c))
	return raw + "." + s.sign(raw)
}
func (s *Service) unpack(raw string, p access.Principal, w, mode string) (cursor, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(s.sign(parts[0])), []byte(parts[1])) {
		return cursor{}, errCursor
	}
	value, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return cursor{}, errCursor
	}
	var c cursor
	if json.Unmarshal(value, &c) != nil || c.User != p.UserID || c.Workspace != w || c.Mode != mode {
		return cursor{}, errCursor
	}
	return c, nil
}
func target(i contexts.Identity) string {
	if i.Kind == "sentence" || i.Kind == "paragraph" || i.Kind == "section" || i.Kind == "document" {
		return "context:" + i.ID
	}
	return i.Kind + ":" + i.ID
}
func denied(err error) bool {
	return errors.Is(err, access.ErrForbidden) || err != nil && (err.Error() == "forbidden" || err.Error() == "missing_reference")
}
func (s *Service) allowed(ctx context.Context, p access.Principal, w string, i contexts.Identity) error {
	if i.Kind == "report" {
		_, err := s.readReport(ctx, p, w, i.ID, "")
		return err
	}
	if i.Kind == "manifest" || i.Kind == "addition" {
		_, err := s.Contexts.Read(ctx, p, w, contexts.Ref{Kind: i.Kind, ID: i.ID, Revision: "head"})
		return err
	}
	return s.Access.Check(ctx, p, w, target(i), "get")
}
func (s *Service) item(ctx context.Context, p access.Principal, w string, r contexts.Ref) (Item, error) {
	var record contexts.Record
	var err error
	if r.Kind == "report" {
		record, err = s.readReport(ctx, p, w, r.ID, r.Revision)
	} else {
		record, err = s.Contexts.Read(ctx, p, w, r)
	}
	if err != nil {
		return Item{}, err
	}
	editable := r.Kind != "report" && r.Kind != "manifest" && r.Kind != "addition" && s.Access.Check(ctx, p, w, target(r.Identity()), "update") == nil
	return Item{record, editable}, nil
}
func (s *Service) Pull(ctx context.Context, p access.Principal, w string, request Request) (apicontract.Response[Frame], error) {
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return apicontract.Response[Frame]{}, err
	}
	if len(request.Known) > pageSize || (request.Mode != "snapshot" && request.Mode != "changes") {
		return apicontract.Response[Frame]{}, errInput
	}
	frame := Frame{Items: []Item{}, Removed: []contexts.Identity{}, Permissions: []Permission{}, Offered: []contexts.Identity{}}
	known := map[string]bool{}
	for _, i := range request.Known {
		known[target(i)] = true
		if err := s.allowed(ctx, p, w, i); err != nil {
			if denied(err) {
				frame.Removed = append(frame.Removed, i)
			} else {
				return apicontract.Response[Frame]{}, err
			}
		} else {
			editable := i.Kind != "report" && i.Kind != "manifest" && i.Kind != "addition" && s.Access.Check(ctx, p, w, target(i), "update") == nil
			frame.Permissions = append(frame.Permissions, Permission{i, editable})
		}
	}
	c := cursor{Workspace: w, User: p.UserID, Mode: request.Mode}
	if request.Cursor != "" {
		var err error
		c, err = s.unpack(request.Cursor, p, w, request.Mode)
		if err != nil {
			return apicontract.Response[Frame]{}, err
		}
	}
	meta := apicontract.Meta{WorkspaceID: w, Completeness: apicontract.Completeness{State: "complete"}}
	if request.Mode == "snapshot" {
		if request.Cursor == "" {
			if err := s.Store.Database().QueryRow(ctx, "SELECT position,sequence FROM shared_streams WHERE workspace_id=$1", w).Scan(&c.Position, &c.Sequence); err != nil {
				return apicontract.Response[Frame]{}, err
			}
		}
		rows, err := s.Store.Database().Query(ctx, `SELECT DISTINCT ON(resource_id) resource_id,kind,revision FROM (
   SELECT resource_id,kind,revision,stream_sequence FROM context_records WHERE workspace_id=$1
   UNION ALL SELECT 'report:'||topic_id,'report',revision,stream_sequence FROM sync_report_versions WHERE workspace_id=$1
  ) r WHERE stream_sequence<=$2 AND resource_id>$3 ORDER BY resource_id,stream_sequence DESC LIMIT 101`, w, c.Sequence, c.After)
		if err != nil {
			return apicontract.Response[Frame]{}, err
		}
		type entry struct{ id, kind, revision string }
		entries := []entry{}
		for rows.Next() {
			var e entry
			if err = rows.Scan(&e.id, &e.kind, &e.revision); err != nil {
				rows.Close()
				return apicontract.Response[Frame]{}, err
			}
			entries = append(entries, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return apicontract.Response[Frame]{}, err
		}
		more := len(entries) > pageSize
		if more {
			entries = entries[:pageSize]
		}
		for _, e := range entries {
			c.After = e.id
			r := contexts.Ref{Kind: e.kind, ID: strings.SplitN(e.id, ":", 2)[1], Revision: e.revision}
			item, err := s.item(ctx, p, w, r)
			if err != nil {
				if denied(err) {
					continue
				}
				return apicontract.Response[Frame]{}, err
			}
			frame.Items = append(frame.Items, item)
		}
		if more {
			next := s.pack(c)
			meta.NextCursor = &next
			meta.Completeness = apicontract.Completeness{State: "partial", Reason: "page_remaining"}
		}
	} else {
		after := request.Position
		if request.Cursor != "" {
			after = c.After
		}
		if after == "" {
			return apicontract.Response[Frame]{}, errHistory
		}
		if request.Cursor == "" && request.Through != "" {
			after = "c." + base64.RawURLEncoding.EncodeToString(encode(map[string]string{"Workspace": w, "After": after, "Through": request.Through}))
		}
		result, err := s.Store.Changes(ctx, persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true}}, w, after, pageSize)
		if err != nil {
			return apicontract.Response[Frame]{}, err
		}
		if result.Error != nil {
			if result.Meta.Completeness.Reason == "history_unavailable" {
				return apicontract.Response[Frame]{}, errHistory
			}
			return apicontract.Response[Frame]{}, errors.New(result.Error.Code)
		}
		c.Position = *result.Meta.Position
		meta = result.Meta
		for _, change := range *result.Data {
			if !strings.HasPrefix(change.Kind, "context-") && change.Kind != "sync-report" {
				continue
			}
			kind := strings.TrimPrefix(change.Kind, "context-")
			if change.Kind == "sync-report" {
				kind = "report"
			}
			r := contexts.Ref{Kind: kind, ID: strings.SplitN(change.ID, ":", 2)[1], Revision: change.Version}
			if change.Kind == "sync-report" {
				var report contexts.Record
				if json.Unmarshal(change.Payload, &report) != nil {
					return apicontract.Response[Frame]{}, errors.New("invalid report record")
				}
				r = report.Ref
			}
			if !known[target(r.Identity())] {
				if s.allowed(ctx, p, w, r.Identity()) == nil {
					frame.Offered = append(frame.Offered, r.Identity())
				}
				continue
			}
			if change.Deleted {
				frame.Removed = append(frame.Removed, r.Identity())
				continue
			}
			item, err := s.item(ctx, p, w, r)
			if err != nil {
				if denied(err) {
					frame.Removed = append(frame.Removed, r.Identity())
					continue
				}
				return apicontract.Response[Frame]{}, err
			}
			frame.Items = append(frame.Items, item)
		}
		if result.Meta.NextCursor != nil {
			c.After = *result.Meta.NextCursor
			next := s.pack(c)
			meta.NextCursor = &next
		}
	}
	meta.Position = &c.Position
	return apicontract.Response[Frame]{Data: &frame, Meta: meta}, nil
}
