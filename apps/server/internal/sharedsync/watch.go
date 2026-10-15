package sharedsync

import (
	"context"
	"errors"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/jackc/pgx/v5"
)

// watch checks only the explicitly displayed or locally edited identities. Its
// cost is independent of unrelated Workspace changes and retained journal pages.
func (s *Service) watch(ctx context.Context, p access.Principal, w string, r Request) (apicontract.Response[Frame], error) {
	if len(r.Known) > pageSize || len(r.Versions) > pageSize || r.Cursor != "" {
		return apicontract.Response[Frame]{}, errInput
	}
	versions := map[contexts.Identity]string{}
	for _, ref := range r.Versions {
		versions[ref.Identity()] = ref.Revision
	}
	frame := Frame{Items: []Item{}, Removed: []contexts.Identity{}, Permissions: []Permission{}, Offered: []contexts.Identity{}}
	for _, id := range r.Known {
		if id.ID == "" || id.Kind == "" {
			return apicontract.Response[Frame]{}, errInput
		}
		if err := s.allowed(ctx, p, w, id); err != nil {
			if denied(err) {
				frame.Removed = append(frame.Removed, id)
				continue
			}
			return apicontract.Response[Frame]{}, err
		}

		if id.Kind == "report" {
			item, err := s.item(ctx, p, w, contexts.Ref{Kind: "report", ID: id.ID, Revision: ""})
			if err != nil {
				if denied(err) {
					frame.Removed = append(frame.Removed, id)
					continue
				}
				return apicontract.Response[Frame]{}, err
			}
			frame.Permissions = append(frame.Permissions, Permission{id, false})
			if versions[id] != item.Record.Ref.Revision {
				frame.Items = append(frame.Items, item)
			}
			continue
		}
		var revision string
		var deleted bool
		err := s.Store.Database().QueryRow(ctx, "SELECT version,deleted FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", w, target(id)).Scan(&revision, &deleted)
		if errors.Is(err, pgx.ErrNoRows) || deleted {
			frame.Removed = append(frame.Removed, id)
			continue
		}
		if err != nil {
			return apicontract.Response[Frame]{}, err
		}
		editable := id.Kind != "report" && id.Kind != "manifest" && id.Kind != "addition" && s.Access.Check(ctx, p, w, target(id), "update") == nil
		frame.Permissions = append(frame.Permissions, Permission{id, editable})
		if versions[id] == revision {
			continue
		}
		item, err := s.item(ctx, p, w, contexts.Ref{Kind: id.Kind, ID: id.ID, Revision: "head"})
		if err != nil {
			if denied(err) {
				frame.Removed = append(frame.Removed, id)
				continue
			}
			return apicontract.Response[Frame]{}, err
		}
		frame.Items = append(frame.Items, item)
	}
	var position string
	if err := s.Store.Database().QueryRow(ctx, "SELECT position FROM shared_streams WHERE workspace_id=$1", w).Scan(&position); err != nil {
		return apicontract.Response[Frame]{}, err
	}
	return apicontract.Response[Frame]{Data: &frame, Meta: apicontract.Meta{WorkspaceID: w, Position: &position, Completeness: apicontract.Completeness{State: "complete"}}}, nil
}
