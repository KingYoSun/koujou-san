package mcpapi

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/clientops"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/repositories"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"strings"
	"time"
)

type InspectionRequest struct {
	Kind  string `json:"kind,omitempty"`
	After string `json:"after,omitempty"`
}
type Inspection struct {
	Workspace  string        `json:"workspace"`
	Project    clientops.Ref `json:"project"`
	ObservedAt time.Time     `json:"observed_at"`
	Kind       string        `json:"kind"`
	Data       any           `json:"data"`
	Next       string        `json:"next,omitempty"`
}

func (s *Service) InspectProject(ctx context.Context, p access.Principal, w string, req InspectionRequest) (Inspection, []clientops.Ref, error) {
	out := Inspection{Workspace: w, ObservedAt: time.Now().UTC(), Kind: req.Kind}
	op, thread, reader, err := s.ClientOps.ChatInspector(ctx, p, w)
	if err != nil {
		return out, nil, err
	}
	out.Project = thread.Project
	refs := []clientops.Ref{thread.Project}
	switch req.Kind {
	case "", "overview":
		project, err := s.Contexts.Read(ctx, reader, w, contexts.Ref{Kind: "scope", ID: strings.TrimPrefix(thread.Project.ID, "scope:"), Revision: thread.Project.Version})
		if err != nil {
			return out, nil, err
		}
		var name, role string
		var execute bool
		if err = s.ClientOps.Store.Database().QueryRow(ctx, `SELECT w.name,m.role,m.can_execute FROM access_workspaces w JOIN access_members m ON m.workspace_id=w.id WHERE w.id=$1 AND m.user_id=$2 AND m.active`, w, p.UserID).Scan(&name, &role, &execute); err != nil {
			return out, nil, err
		}
		out.Data = map[string]any{"workspace_name": name, "project": project.Value, "role": role, "can_execute": execute, "model": op.Model, "settings": map[string]string{"repositories": "repositories", "runner": "execution", "devices": "devices", "models": "models"}}
	case "repositories":
		if s.Repositories == nil {
			return out, nil, clientops.ErrState
		}
		page, err := s.Repositories.Repositories(ctx, reader, w, strings.TrimPrefix(thread.Project.ID, "scope:"), req.After)
		if err != nil {
			return out, nil, err
		}
		out.Data, out.Next = page.Items, page.Next
		for _, r := range page.Items {
			refs = append(refs, clientops.Ref{ID: "forge-repository:" + r.ID, Version: r.Version}, clientops.Ref{ID: "forge-connection:" + r.Connection, Version: r.ConnectionVersion})
		}
	case "runners":
		rows, err := s.ClientOps.Store.Database().Query(ctx, `SELECT r.payload,d.enabled,d.can_input,d.can_report,v.version FROM run_runners r JOIN access_devices d ON d.workspace_id=r.workspace_id AND d.id=r.device_id JOIN shared_versions v ON v.workspace_id=d.workspace_id AND v.resource_id='device:'||d.id WHERE r.workspace_id=$1 AND r.device_id>$4 AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id','runner:'||r.device_id,'version',r.version),jsonb_build_object('id','device:'||r.device_id,'version',v.version))`)+` ORDER BY r.device_id LIMIT 51`, w, reader.UserID, reader.OperationID, req.After)
		if err != nil {
			return out, nil, err
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var data []byte
			var enabled, input, report bool
			var version string
			if err := rows.Scan(&data, &enabled, &input, &report, &version); err != nil {
				return out, nil, err
			}
			var runner runs.Runner
			if json.Unmarshal(data, &runner) != nil {
				return out, nil, clientops.ErrInvalid
			}
			if len(items) == 50 {
				out.Next = items[49].(map[string]any)["runner"].(runs.Runner).DeviceID
				break
			}
			items = append(items, map[string]any{"runner": runner, "enabled": enabled, "can_input": input, "can_report": report, "contact_current": out.ObservedAt.Before(runner.ExpiresAt)})
			refs = append(refs, runner.Ref, clientops.Ref{ID: "device:" + runner.DeviceID, Version: version})
		}
		if err = rows.Err(); err != nil {
			return out, nil, err
		}
		out.Data = items
	case "runs":
		page, err := s.Runs.Query(ctx, reader, w, runs.Request{Kind: "run", After: req.After, Limit: 50})
		if err != nil || page.Data == nil {
			return out, nil, err
		}
		out.Data, out.Next = page.Data.Items, page.Data.Next
		for _, r := range page.Data.Items {
			refs = append(refs, r.Ref)
			refs = append(refs, r.Refs...)
		}
	default:
		return out, nil, clientops.ErrInvalid
	}
	return out, refs, s.ClientOps.InspectionRefs(ctx, p, w, op, refs)
}

func (s *Service) ReadRepository(ctx context.Context, p access.Principal, w string, req repositories.ContentRequest) (repositories.Content, []clientops.Ref, error) {
	op, thread, reader, err := s.ClientOps.ChatInspector(ctx, p, w)
	if err != nil || s.Repositories == nil {
		return repositories.Content{}, nil, access.ErrForbidden
	}
	r, err := s.Repositories.Read(ctx, reader, w, req.Repository)
	if err != nil || r.Project != strings.TrimPrefix(thread.Project.ID, "scope:") {
		return repositories.Content{}, nil, access.ErrForbidden
	}
	value, err := s.Repositories.ReadContent(ctx, reader, w, req)
	if err != nil {
		return value, nil, err
	}
	refs := []clientops.Ref{thread.Project, {ID: "forge-repository:" + r.ID, Version: r.Version}, {ID: "forge-connection:" + r.Connection, Version: r.ConnectionVersion}}
	return value, refs, s.ClientOps.InspectionRefs(ctx, p, w, op, refs)
}
