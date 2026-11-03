package access

import (
	"context"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
)

type ResourcePolicy struct {
	ID         string        `json:"id"`
	Restricted bool          `json:"restricted"`
	Version    string        `json:"version"`
	Grants     []PolicyGrant `json:"grants"`
	Next       string        `json:"next,omitempty"`
}
type PolicyGrant struct {
	User    string `json:"user"`
	Name    string `json:"name"`
	Read    bool   `json:"read"`
	Update  bool   `json:"update"`
	Execute bool   `json:"execute"`
}
type PolicyWrite struct {
	apicontract.Mutation
	Restricted *bool        `json:"restricted,omitempty"`
	Grant      *PolicyGrant `json:"grant,omitempty"`
}

func (s *Service) ResourcePolicy(ctx context.Context, p Principal, w, id, after string) (ResourcePolicy, error) {
	out := ResourcePolicy{ID: id, Grants: []PolicyGrant{}}
	if err := s.Check(ctx, p, w, "", "manage"); err != nil {
		return out, err
	}
	err := s.DB.QueryRow(ctx, `SELECT r.restricted,COALESCE(v.version,'0') FROM access_resources r LEFT JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id='resource-policy:'||r.id WHERE r.workspace_id=$1 AND r.id=$2`, w, id).Scan(&out.Restricted, &out.Version)
	if err != nil {
		return out, err
	}
	rows, err := s.DB.Query(ctx, `SELECT g.user_id,u.display_name,g.can_read,g.can_update,g.can_execute FROM access_resource_grants g JOIN auth_users u ON u.id=g.user_id WHERE g.workspace_id=$1 AND g.resource_id=$2 AND g.user_id>$3 ORDER BY g.user_id LIMIT 51`, w, id, after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g PolicyGrant
		if err = rows.Scan(&g.User, &g.Name, &g.Read, &g.Update, &g.Execute); err != nil {
			return out, err
		}
		out.Grants = append(out.Grants, g)
	}
	if len(out.Grants) > 50 {
		out.Grants = out.Grants[:50]
		out.Next = out.Grants[49].User
	}
	return out, rows.Err()
}

func (s *Service) PutResourcePolicy(ctx context.Context, p Principal, w, id string, req PolicyWrite) (apicontract.Response[apicontract.Reference], error) {
	if (req.Restricted == nil) == (req.Grant == nil) || id == "" || (req.Grant != nil && (req.Grant.User == "" || (!req.Grant.Read && (req.Grant.Update || req.Grant.Execute)))) {
		return apicontract.Response[apicontract.Reference]{}, errors.New("invalid_resource_policy")
	}
	return s.mutate(ctx, p, w, "resource-policy:"+id, "resource-policy", "manage", req.Mutation, req, func(tx pgx.Tx) (any, error) {
		var restricted bool
		if err := tx.QueryRow(ctx, `SELECT restricted FROM access_resources WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).Scan(&restricted); err != nil {
			return nil, err
		}
		if req.Restricted != nil {
			_, err := tx.Exec(ctx, `UPDATE access_resources SET restricted=$3 WHERE workspace_id=$1 AND id=$2`, w, id, *req.Restricted)
			return req, err
		}
		g := req.Grant
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM access_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, w, g.User).Scan(&active); err != nil || !active {
			return nil, ErrForbidden
		}
		_, err := tx.Exec(ctx, `INSERT INTO access_resource_grants VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,resource_id,user_id) DO UPDATE SET can_read=excluded.can_read,can_update=excluded.can_update,can_execute=excluded.can_execute`, w, id, g.User, g.Read, g.Update, g.Execute)
		return req, err
	})
}

func (s *Service) registerResourcePolicy(mux *http.ServeMux) {
	base := "/access/workspaces/{workspace}/resources/{resource}"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		result, err := s.ResourcePolicy(r.Context(), p, r.PathValue("workspace"), r.PathValue("resource"), r.URL.Query().Get("after"))
		if err != nil {
			fail(w, r.PathValue("workspace"), err)
			return
		}
		send(w, 200, result)
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.user(w, r)
		if !ok {
			return
		}
		var req PolicyWrite
		if !decode(w, r, &req) {
			return
		}
		result, err := s.PutResourcePolicy(r.Context(), p, r.PathValue("workspace"), r.PathValue("resource"), req)
		if err != nil {
			fail(w, r.PathValue("workspace"), err)
			return
		}
		status := 200
		if result.Error != nil {
			status = 409
			if result.Error.Code == "forbidden" {
				status = 403
			}
		}
		send(w, status, result)
	})
}
