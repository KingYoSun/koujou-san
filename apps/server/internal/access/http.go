package access

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

type command struct {
	apicontract.Mutation
	ID         string `json:"id"`
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Role       string `json:"role"`
	Execute    bool   `json:"can_execute"`
	Active     bool   `json:"active"`
	Input      bool   `json:"can_input"`
	Report     bool   `json:"can_report"`
	Token      string `json:"token"`
	Target     string `json:"target"`
	Action     string `json:"action"`
}
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next_cursor,omitempty"`
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, workspace string, err error) {
	code, status := "unavailable", 503
	if errors.Is(err, ErrForbidden) {
		code, status = "forbidden", 403
	}
	send(w, status, apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: "not_accessed"}}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		send(w, 400, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /access/workspaces", s.listWorkspaces)
	mux.HandleFunc("GET /access/workspaces/{workspace}/{kind}", s.list)
	mux.HandleFunc("POST /access/workspaces/{workspace}/{kind}", s.write)
}
func (s *Service) user(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	id, ok := s.User(w, r)
	return Principal{UserID: id}, ok
}
func (s *Service) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	p, ok := s.user(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT w.id,w.name,m.role,m.can_execute,COALESCE(v.version,'0') FROM access_workspaces w JOIN access_members m ON m.workspace_id=w.id AND m.user_id=$1 AND m.active LEFT JOIN shared_versions v ON v.workspace_id=w.id AND v.resource_id='workspace:'||w.id WHERE w.id>$2 ORDER BY w.id LIMIT 51`, p.UserID, r.URL.Query().Get("after"))
	if err != nil {
		fail(w, "", err)
		return
	}
	defer rows.Close()
	page := Page[Workspace]{Items: []Workspace{}}
	for rows.Next() {
		var item Workspace
		if err = rows.Scan(&item.ID, &item.Name, &item.Role, &item.Execute, &item.Version); err != nil {
			fail(w, "", err)
			return
		}
		page.Items = append(page.Items, item)
	}
	if rows.Err() != nil {
		fail(w, "", rows.Err())
		return
	}
	if len(page.Items) > 50 {
		page.Items = page.Items[:50]
		page.Next = page.Items[49].ID
	}
	send(w, 200, page)
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("kind") == "device-state" {
		workspace := r.PathValue("workspace")
		p, err := s.DevicePrincipal(r.Context(), workspace, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err == nil {
			err = s.Check(r.Context(), p, workspace, "", "contact")
		}
		if err != nil {
			fail(w, workspace, err)
			return
		}
		d, _, err := s.device(r.Context(), s.DB, workspace, p.DeviceID)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, 200, map[string]any{"device": d})
		return
	}
	p, ok := s.user(w, r)
	if !ok {
		return
	}
	workspace, kind := r.PathValue("workspace"), r.PathValue("kind")
	ctx := r.Context()
	if err := s.Check(ctx, p, workspace, "", "read"); err != nil {
		fail(w, workspace, err)
		return
	}
	if kind == "mine" {
		var id string
		err := s.DB.QueryRow(ctx, "SELECT id FROM access_devices WHERE workspace_id=$1 AND registered_by=$2 AND identifier=$3", workspace, p.UserID, r.URL.Query().Get("identifier")).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			send(w, 200, map[string]any{"device": nil})
			return
		}
		if err != nil {
			fail(w, workspace, err)
			return
		}
		d, _, err := s.device(ctx, s.DB, workspace, id)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, 200, map[string]any{"device": d})
		return
	}
	after := r.URL.Query().Get("after")
	items := []any{}
	next := ""
	var rows pgx.Rows
	var err error
	switch kind {
	case "members":
		rows, err = s.DB.Query(ctx, `SELECT m.user_id,u.display_name,m.role,m.can_execute,m.active,COALESCE(v.version,'0') FROM access_members m JOIN auth_users u ON u.id=m.user_id LEFT JOIN shared_versions v ON v.workspace_id=m.workspace_id AND v.resource_id='member:'||m.user_id WHERE m.workspace_id=$1 AND m.user_id>$2 ORDER BY m.user_id LIMIT 51`, workspace, after)
	case "invites":
		if err = s.Check(ctx, p, workspace, "", "manage"); err != nil {
			fail(w, workspace, err)
			return
		}
		rows, err = s.DB.Query(ctx, `SELECT i.id,i.role,i.expires_at,i.cancelled,i.accepted_by IS NOT NULL,COALESCE(v.version,'0') FROM access_invites i LEFT JOIN shared_versions v ON v.workspace_id=i.workspace_id AND v.resource_id='invite:'||i.id WHERE i.workspace_id=$1 AND i.id>$2 ORDER BY i.id LIMIT 51`, workspace, after)
	case "devices":
		rows, err = s.DB.Query(ctx, `SELECT d.id,d.name,d.identifier,d.registered_by,d.enabled,d.can_input,d.can_report,d.last_seen,COALESCE(v.version,'0') FROM access_devices d LEFT JOIN shared_versions v ON v.workspace_id=d.workspace_id AND v.resource_id='device:'||d.id WHERE d.workspace_id=$1 AND d.id>$2 ORDER BY d.id LIMIT 51`, workspace, after)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, workspace, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var item any
		var id string
		switch kind {
		case "members":
			var m Member
			err = rows.Scan(&m.ID, &m.Name, &m.Role, &m.Execute, &m.Active, &m.Version)
			item, id = m, m.ID
		case "invites":
			var i Invite
			err = rows.Scan(&i.ID, &i.Role, &i.Expires, &i.Cancelled, &i.Accepted, &i.Version)
			item, id = i, i.ID
		case "devices":
			var d Device
			err = rows.Scan(&d.ID, &d.Name, &d.Identifier, &d.RegisteredBy, &d.Enabled, &d.Input, &d.Report, &d.LastSeen, &d.Version)
			item, id = d, d.ID
		}
		if err != nil {
			fail(w, workspace, err)
			return
		}
		if len(items) == 50 {
			next = after
			break
		}
		items = append(items, item)
		after = id
	}
	if rows.Err() != nil {
		fail(w, workspace, rows.Err())
		return
	}
	state := "complete"
	if next != "" {
		state = "partial"
	}
	page := Page[any]{Items: items, Next: next}
	send(w, 200, apicontract.Response[Page[any]]{Data: &page, Meta: apicontract.Meta{WorkspaceID: workspace, NextCursor: func() *string {
		if next == "" {
			return nil
		}
		return &next
	}(), Completeness: apicontract.Completeness{State: state}}})
}
func (s *Service) write(w http.ResponseWriter, r *http.Request) {
	workspace, kind := r.PathValue("workspace"), r.PathValue("kind")
	ctx := r.Context()
	var c command
	if !decode(w, r, &c) {
		return
	}
	var p Principal
	if kind == "contact" || (kind == "authorize" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer d.")) {
		var err error
		p, err = s.DevicePrincipal(ctx, workspace, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			fail(w, workspace, err)
			return
		}
	} else {
		var ok bool
		p, ok = s.user(w, r)
		if !ok {
			return
		}
	}
	if kind == "authorize" {
		if err := s.Check(ctx, p, workspace, c.Target, c.Action); err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, 200, map[string]bool{"allowed": true})
		return
	}
	if kind == "invite-accept" {
		s.accept(w, r, p, workspace, c)
		return
	}
	if kind == "invite-preview" {
		_, version, err := s.invitation(ctx, s.DB, workspace, c.Token, p.UserID)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, 200, map[string]string{"version": version})
		return
	}
	action, id := "manage", c.ID
	switch kind {
	case "create":
		action, id = "create", workspace
	case "leave":
		action, id = "leave", "member:"+p.UserID
	case "member-update":
		id = "member:" + c.ID
	case "device-register", "device-rotate":
		action = "register"
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			fail(w, workspace, ErrForbidden)
			return
		}
	case "contact":
		action, id = "contact", p.DeviceID
	case "invite-create", "invite-cancel", "device-update":
	default:
		http.NotFound(w, r)
		return
	}
	if id == "" || ((kind == "create" || kind == "device-register") && strings.TrimSpace(c.Name) == "") || ((kind == "invite-create" || kind == "member-update") && !validRole(c.Role)) || (kind == "device-register" && c.Identifier == "") {
		send(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	result, err := s.mutate(ctx, p, workspace, id, kind, action, c.Mutation, c, func(tx pgx.Tx) (any, error) { return s.apply(ctx, tx, p, workspace, kind, id, c) })
	if err != nil {
		fail(w, workspace, err)
		return
	}
	status := 200
	if result.Error != nil {
		switch result.Error.Code {
		case "forbidden":
			status = 403
		case "unavailable":
			status = 503
		default:
			status = 409
		}
	}
	if result.Error == nil && (kind == "device-register" || kind == "device-rotate") {
		d, generation, err := s.device(ctx, s.DB, workspace, id)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, status, map[string]any{"result": result, "device": d, "credential": s.deviceToken(id, generation)})
		return
	}
	if result.Error == nil && kind == "invite-create" {
		send(w, status, map[string]any{"result": result, "invitation": workspace + "." + s.InviteToken(id)})
		return
	}
	send(w, status, result)
}

func (s *Service) apply(ctx context.Context, tx pgx.Tx, p Principal, w, kind, id string, c command) (any, error) {
	switch kind {
	case "create":
		if _, err := tx.Exec(ctx, "INSERT INTO access_workspaces VALUES($1,$2,$3)", w, c.Name, p.UserID); err != nil {
			return nil, err
		}
		_, err := tx.Exec(ctx, "INSERT INTO access_members(workspace_id,user_id,role) VALUES($1,$2,'admin')", w, p.UserID)
		return map[string]string{"id": w, "name": c.Name}, err
	case "invite-create":
		expires := time.Now().Add(7 * 24 * time.Hour)
		_, err := tx.Exec(ctx, "INSERT INTO access_invites(id,workspace_id,role,expires_at) VALUES($1,$2,$3,$4)", id, w, c.Role, expires)
		return map[string]any{"id": id, "role": c.Role, "expires_at": expires}, err
	case "invite-cancel":
		tag, err := tx.Exec(ctx, "UPDATE access_invites SET cancelled=true WHERE workspace_id=$1 AND id=$2", w, id)
		if err == nil && tag.RowsAffected() != 1 {
			err = ErrForbidden
		}
		return map[string]any{"id": id, "cancelled": true}, err
	case "leave", "member-update":
		u := c.ID
		role, execute, active := c.Role, c.Execute, c.Active
		if kind == "leave" {
			u = p.UserID
			m, err := s.member(ctx, tx, w, u)
			if err != nil {
				return nil, err
			}
			role, execute, active = m.Role, m.Execute, false
		}
		tag, err := tx.Exec(ctx, "UPDATE access_members SET role=$3,can_execute=$4,active=$5 WHERE workspace_id=$1 AND user_id=$2", w, u, role, execute, active)
		if err == nil && tag.RowsAffected() != 1 {
			err = ErrForbidden
		}
		if err == nil && !active {
			err = s.revokeOwner(ctx, tx, w, u)
		}
		return map[string]any{"id": u, "role": role, "can_execute": execute, "active": active}, err
	case "device-register":
		_, err := tx.Exec(ctx, "INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation) VALUES($1,$2,$3,$4,$5,$6)", id, w, p.UserID, c.Identifier, c.Name, randomGeneration())
		return map[string]any{"id": id, "name": c.Name, "registered_by": p.UserID, "enabled": true, "can_input": false, "can_report": false}, err
	case "device-rotate":
		d, _, err := s.device(ctx, tx, w, id)
		if err != nil {
			return nil, err
		}
		if d.RegisteredBy != p.UserID {
			return nil, ErrForbidden
		}
		_, err = tx.Exec(ctx, "UPDATE access_devices SET generation=$3,enabled=true,can_input=CASE WHEN enabled THEN can_input ELSE false END,can_report=CASE WHEN enabled THEN can_report ELSE false END WHERE workspace_id=$1 AND id=$2", w, id, randomGeneration())
		return map[string]any{"id": id, "credential_updated": true}, err
	case "device-update":
		if c.Active {
			d, _, err := s.device(ctx, tx, w, id)
			if err != nil {
				return nil, err
			}
			if _, err = s.member(ctx, tx, w, d.RegisteredBy); err != nil {
				return nil, err
			}
		}
		tag, err := tx.Exec(ctx, "UPDATE access_devices SET enabled=$3,can_input=$4,can_report=$5,generation=CASE WHEN $3 THEN generation ELSE $6 END WHERE workspace_id=$1 AND id=$2", w, id, c.Active, c.Input, c.Report, randomGeneration())
		if err == nil && tag.RowsAffected() != 1 {
			err = ErrForbidden
		}
		return map[string]any{"id": id, "enabled": c.Active, "can_input": c.Input, "can_report": c.Report}, err
	case "contact":
		_, err := tx.Exec(ctx, "UPDATE access_devices SET last_seen=clock_timestamp() WHERE workspace_id=$1 AND id=$2", w, id)
		d, _, readErr := s.device(ctx, tx, w, id)
		if err == nil {
			err = readErr
		}
		return map[string]any{"id": d.ID, "last_seen": d.LastSeen}, err
	}
	return nil, ErrForbidden
}

func (s *Service) invitation(ctx context.Context, q query, workspace, token, user string) (string, string, error) {
	id, signature, ok := tokenParts(token)
	if !ok || !hmac.Equal([]byte(signature), []byte(s.proof("invite", id, ""))) {
		return "", "", ErrForbidden
	}
	var cancelled bool
	var expires time.Time
	var accepted *string
	var version string
	err := q.QueryRow(ctx, `SELECT i.cancelled,i.expires_at,i.accepted_by,COALESCE(v.version,'0') FROM access_invites i LEFT JOIN shared_versions v ON v.workspace_id=i.workspace_id AND v.resource_id='invite:'||i.id WHERE i.workspace_id=$1 AND i.id=$2`, workspace, id).Scan(&cancelled, &expires, &accepted, &version)
	if err != nil {
		return "", "", deny(err)
	}
	if cancelled || time.Now().After(expires) || (accepted != nil && *accepted != user) {
		return "", "", ErrForbidden
	}
	if accepted != nil {
		if _, err = s.member(ctx, q, workspace, user); err != nil {
			return "", "", err
		}
	}
	return id, version, nil
}
func (s *Service) accept(w http.ResponseWriter, r *http.Request, p Principal, workspace string, c command) {
	id, _, err := s.invitation(r.Context(), s.DB, workspace, c.Token, p.UserID)
	if err != nil {
		fail(w, workspace, err)
		return
	}
	// Acceptance is the only policy that admits a current non-member. Validate it
	// again under the same stream lock before Apply can replay a receipt.
	check := func(q query) error {
		_, _, err := s.invitation(r.Context(), q, workspace, c.Token, p.UserID)
		return err
	}
	if err := check(s.DB); err != nil {
		fail(w, workspace, err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"invite_id": id, "user_id": p.UserID})
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: workspace, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := check(tx); err != nil {
			code := "unavailable"
			if errors.Is(err, ErrForbidden) {
				code = "forbidden"
			}
			return &apicontract.Problem{Code: code}
		}
		return nil
	}}
	result, err := s.Store.Apply(r.Context(), scope, persistence.Command{WorkspaceID: workspace, ResourceID: "invite:" + id, Kind: "invite-accept", Mutation: c.Mutation, Payload: payload}, func(tx pgx.Tx, _ apicontract.Reference) (json.RawMessage, error) {
		var role string
		var accepted *string
		if err := tx.QueryRow(r.Context(), "SELECT role,accepted_by FROM access_invites WHERE workspace_id=$1 AND id=$2", workspace, id).Scan(&role, &accepted); err != nil {
			return nil, err
		}
		if accepted != nil {
			return nil, ErrForbidden
		}
		if _, err := tx.Exec(r.Context(), "INSERT INTO access_members(workspace_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=EXCLUDED.role,can_execute=false,active=true", workspace, p.UserID, role); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE access_invites SET accepted_by=$3 WHERE workspace_id=$1 AND id=$2", workspace, id, p.UserID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"id": id, "accepted_by": p.UserID, "role": role})
	})
	if err != nil {
		fail(w, workspace, err)
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
}
