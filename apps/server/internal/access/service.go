// Package access owns current Workspace membership and device authorization.
package access

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("forbidden")

type query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Service struct {
	Store *persistence.Store
	DB    *pgxpool.Pool
	key   []byte
	User  func(http.ResponseWriter, *http.Request) (string, bool)
}

func New(store *persistence.Store, key []byte, user func(http.ResponseWriter, *http.Request) (string, bool)) *Service {
	return &Service{store, store.Database(), key, user}
}

type Principal struct {
	UserID      string
	OperationID string
	DeviceID    string
	generation  string
}
type Workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Execute bool   `json:"can_execute"`
	Version string `json:"version"`
}
type Member struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Execute bool   `json:"can_execute"`
	Active  bool   `json:"active"`
	Version string `json:"version"`
}
type Invite struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Expires   time.Time `json:"expires_at"`
	Cancelled bool      `json:"cancelled"`
	Accepted  bool      `json:"accepted"`
	Version   string    `json:"version"`
}
type Device struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Identifier   string     `json:"identifier"`
	RegisteredBy string     `json:"registered_by"`
	Enabled      bool       `json:"enabled"`
	Input        bool       `json:"can_input"`
	Report       bool       `json:"can_report"`
	LastSeen     *time.Time `json:"last_seen"`
	Version      string     `json:"version"`
}
type Resource struct {
	WorkspaceID, ID string
	Restricted      bool
	AssignedDevice  string
}
type ResourceGrant struct {
	UserID                string
	Read, Update, Execute bool
}

func validRole(role string) bool { return role == "admin" || role == "editor" || role == "viewer" }
func (s *Service) proof(kind, id, generation string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(kind + "\x00" + id + "\x00" + generation))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) InviteToken(id string) string { return id + "." + s.proof("invite", id, "") }
func (s *Service) deviceToken(id, generation string) string {
	return "d." + id + "." + s.proof("device", id, generation)
}
func tokenParts(token string) (string, string, bool) {
	p := strings.Split(token, ".")
	if len(p) != 2 {
		return "", "", false
	}
	return p[0], p[1], true
}
func deny(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	return err
}
func (s *Service) device(ctx context.Context, q query, w, id string) (Device, string, error) {
	var d Device
	var generation string
	err := q.QueryRow(ctx, `SELECT d.id,d.name,d.identifier,d.registered_by,d.enabled,d.can_input,d.can_report,d.last_seen,COALESCE(v.version,'0'),d.generation FROM access_devices d LEFT JOIN shared_versions v ON v.workspace_id=d.workspace_id AND v.resource_id='device:'||d.id WHERE d.workspace_id=$1 AND d.id=$2`, w, id).Scan(&d.ID, &d.Name, &d.Identifier, &d.RegisteredBy, &d.Enabled, &d.Input, &d.Report, &d.LastSeen, &d.Version, &generation)
	return d, generation, deny(err)
}
func (s *Service) DevicePrincipal(ctx context.Context, w, token string) (Principal, error) {
	if !strings.HasPrefix(token, "d.") {
		return Principal{}, ErrForbidden
	}
	id, signature, ok := tokenParts(strings.TrimPrefix(token, "d."))
	if !ok {
		return Principal{}, ErrForbidden
	}
	d, generation, err := s.device(ctx, s.DB, w, id)
	if err != nil {
		return Principal{}, err
	}
	if !d.Enabled || !hmac.Equal([]byte(signature), []byte(s.proof("device", id, generation))) {
		return Principal{}, ErrForbidden
	}
	return Principal{DeviceID: id, generation: generation}, nil
}
func (s *Service) member(ctx context.Context, q query, w, u string) (Member, error) {
	var m Member
	err := q.QueryRow(ctx, `SELECT m.user_id,u.display_name,m.role,m.can_execute,m.active,COALESCE(v.version,'0') FROM access_members m JOIN auth_users u ON u.id=m.user_id LEFT JOIN shared_versions v ON v.workspace_id=m.workspace_id AND v.resource_id='member:'||m.user_id WHERE m.workspace_id=$1 AND m.user_id=$2`, w, u).Scan(&m.ID, &m.Name, &m.Role, &m.Execute, &m.Active, &m.Version)
	if err == nil && !m.Active {
		return m, ErrForbidden
	}
	return m, deny(err)
}

// Check resolves the target's actual Workspace and grants in PostgreSQL. A client
// cannot broaden this policy with a parent, reference, Scope or another actor ID.
func (s *Service) Check(ctx context.Context, p Principal, w, target, action string) error {
	return s.check(ctx, s.DB, p, w, target, action)
}

// CheckTx keeps domain reference checks in the owning Workspace transaction.
func (s *Service) CheckTx(ctx context.Context, tx pgx.Tx, p Principal, w, target, action string) error {
	return s.check(ctx, tx, p, w, target, action)
}
func (s *Service) check(ctx context.Context, q query, p Principal, w, target, action string) error {
	if target == "" && (action == "get" || action == "search" || action == "update" || action == "execute" || action == "input" || action == "report") {
		return ErrForbidden
	}
	if p.OperationID != "" {
		op, err := s.operation(ctx, q, p, w, p.OperationID)
		if err != nil {
			return err
		}
		if p.DeviceID != "" || !operationAction(op, action) {
			return ErrForbidden
		}
	}
	if p.DeviceID != "" {
		d, generation, err := s.device(ctx, q, w, p.DeviceID)
		if err != nil {
			return err
		}
		if !d.Enabled || p.generation != generation {
			return ErrForbidden
		}
		if _, err = s.member(ctx, q, w, d.RegisteredBy); err != nil {
			return err
		}
		if action == "contact" {
			return nil
		}
		if (action != "input" || !d.Input) && (action != "report" || !d.Report) {
			return ErrForbidden
		}
		var assigned *string
		if err = q.QueryRow(ctx, "SELECT assigned_device FROM access_resources WHERE workspace_id=$1 AND id=$2", w, target).Scan(&assigned); err != nil {
			return deny(err)
		}
		if assigned == nil || *assigned != p.DeviceID {
			return ErrForbidden
		}
		return nil
	}
	if p.UserID == "" {
		return ErrForbidden
	}
	if action == "create" {
		var creator string
		err := q.QueryRow(ctx, "SELECT created_by FROM access_workspaces WHERE id=$1", w).Scan(&creator)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if creator != p.UserID {
			return ErrForbidden
		}
	}
	m, err := s.member(ctx, q, w, p.UserID)
	if err != nil {
		return err
	}
	switch action {
	case "read", "get", "search", "leave", "create":
	case "manage":
		if m.Role != "admin" {
			return ErrForbidden
		}
	case "update", "register", "price_write":
		if m.Role == "viewer" {
			return ErrForbidden
		}
	case "execute":
		if !m.Execute {
			return ErrForbidden
		}
	default:
		return ErrForbidden
	}
	if target == "" {
		return nil
	}
	if action == "price_write" {
		action = "update"
	}
	allowed, err := resourceGrants(ctx, q, w, p, []string{target}, action)
	if err != nil {
		return err
	}
	if !allowed[target] {
		return ErrForbidden
	}
	return nil
}

// Readable checks a bounded set of graph candidates using the same policy as
// individual resource reads. References and containment never grant access.
func (s *Service) Readable(ctx context.Context, p Principal, w string, ids []string) (map[string]bool, error) {
	if err := s.Check(ctx, p, w, "", "read"); err != nil {
		return nil, err
	}
	return resourceGrants(ctx, s.DB, w, p, ids, "get")
}
func resourceGrants(ctx context.Context, q query, w string, p Principal, ids []string, action string) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT r.id FROM access_resources r LEFT JOIN access_resource_grants g ON g.workspace_id=r.workspace_id AND g.resource_id=r.id AND g.user_id=$3 WHERE r.workspace_id=$1 AND r.id=ANY($2) AND (NOT r.restricted OR (g.can_read AND ($4!='update' OR g.can_update) AND ($4!='execute' OR g.can_execute)))`, w, ids, p.UserID, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if p.OperationID != "" {
		eligible, err := versionGrants(ctx, q, p, w, identityVersionRefs(ids))
		if err != nil {
			return nil, err
		}
		for id := range result {
			if !eligible[VersionRef{ID: id}] {
				delete(result, id)
			}
		}
	}
	return result, nil
}

// PutResource is called in the owning domain's transaction, not through a client
// endpoint. Context/Run services supply their own authoritative resource records.
func (s *Service) PutResource(ctx context.Context, tx pgx.Tx, r Resource, grants []ResourceGrant) error {
	_, err := tx.Exec(ctx, `INSERT INTO access_resources VALUES($1,$2,$3,NULLIF($4,'')) ON CONFLICT(workspace_id,id) DO UPDATE SET restricted=EXCLUDED.restricted,assigned_device=EXCLUDED.assigned_device`, r.WorkspaceID, r.ID, r.Restricted, r.AssignedDevice)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM access_resource_grants WHERE workspace_id=$1 AND resource_id=$2", r.WorkspaceID, r.ID); err != nil {
		return err
	}
	for _, g := range grants {
		if _, err = tx.Exec(ctx, "INSERT INTO access_resource_grants VALUES($1,$2,$3,$4,$5,$6)", r.WorkspaceID, r.ID, g.UserID, g.Read, g.Update, g.Execute); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) mutate(ctx context.Context, p Principal, w, id, kind, action string, m apicontract.Mutation, payload any, write func(pgx.Tx) (any, error)) (apicontract.Response[apicontract.Reference], error) {
	if err := s.Check(ctx, p, w, "", action); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	actor := "user:" + p.UserID
	if p.DeviceID != "" {
		actor = "device:" + p.DeviceID
	}
	scope := persistence.Scope{ActorID: actor, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := s.check(ctx, tx, p, w, "", action); err != nil {
			code := "unavailable"
			if errors.Is(err, ErrForbidden) {
				code = "forbidden"
			}
			return &apicontract.Problem{Code: code}
		}
		return nil
	}}
	storageID := id
	switch {
	case kind == "create":
		storageID = "workspace:" + id
	case strings.HasPrefix(kind, "invite-"):
		storageID = "invite:" + id
	case strings.HasPrefix(kind, "device-") || kind == "contact":
		storageID = "device:" + id
	}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: storageID, Kind: kind, Mutation: m, Payload: data}, func(tx pgx.Tx, _ apicontract.Reference) (json.RawMessage, error) {
		state, err := write(tx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(state)
	})
}
func (s *Service) revokeOwner(ctx context.Context, tx pgx.Tx, w, u string) error {
	_, err := tx.Exec(ctx, "UPDATE access_devices SET enabled=false,generation=$3 WHERE workspace_id=$1 AND registered_by=$2", w, u, randomGeneration())
	return err
}
func randomGeneration() string { return rand.Text() }
