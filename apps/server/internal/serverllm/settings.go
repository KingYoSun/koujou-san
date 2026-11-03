package serverllm

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/jackc/pgx/v5"
)

type Assignment struct {
	Connection
	Allowed bool   `json:"allowed"`
	Version string `json:"version"`
}

func (g *Gateway) Register(mux *http.ServeMux, a *access.Service) {
	// Server-owned credentials are assigned only by a current server administrator.
	authorize := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		user, ok := a.User(w, r)
		if !ok {
			return "", false
		}
		var admin bool
		if g.Store.Database().QueryRow(r.Context(), `SELECT administrator FROM auth_users WHERE id=$1`, user).Scan(&admin) != nil || !admin || a.Check(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), "", "read") != nil {
			http.Error(w, "forbidden", 403)
			return "", false
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		return user, true
	}
	mux.HandleFunc("GET /server-models/workspaces/{workspace}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authorize(w, r); !ok {
			return
		}
		all, err := g.connections("", true)
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		permitted, err := g.Connections(r.PathValue("workspace"))
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		allowed := map[string]bool{}
		for _, c := range permitted {
			allowed[c.ID] = true
		}
		items := []Assignment{}
		next := ""
		// The bounded page is ordered independently of the operator's file order.
		sortConnections(all)
		for _, c := range all {
			if c.ID <= r.URL.Query().Get("after") {
				continue
			}
			if len(items) == 50 {
				next = items[49].ID
				break
			}
			var version int64
			err = g.Store.Database().QueryRow(r.Context(), `SELECT version FROM server_model_workspace_grants WHERE workspace_id=$1 AND connection_id=$2`, r.PathValue("workspace"), c.ID).Scan(&version)
			if err != nil && err != pgx.ErrNoRows {
				http.Error(w, "unavailable", 503)
				return
			}
			items = append(items, Assignment{c, allowed[c.ID], strconv.FormatInt(version, 10)})
		}
		json.NewEncoder(w).Encode(struct {
			Items []Assignment `json:"items"`
			Next  string       `json:"next,omitempty"`
		}{items, next})
	})
	mux.HandleFunc("POST /server-models/workspaces/{workspace}/{connection}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authorize(w, r)
		if !ok {
			return
		}
		var req struct {
			Allowed bool   `json:"allowed"`
			Version string `json:"expected_version"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil {
			http.Error(w, "invalid_request", 400)
			return
		}
		version, err := strconv.ParseInt(req.Version, 10, 64)
		if err != nil || version < 0 {
			http.Error(w, "invalid_version", 400)
			return
		}
		all, err := g.connections("", true)
		found := false
		for _, c := range all {
			if c.ID == r.PathValue("connection") {
				found = true
			}
		}
		if err != nil || !found {
			http.Error(w, "unavailable", 400)
			return
		}
		err = pgx.BeginFunc(r.Context(), g.Store.Database(), func(tx pgx.Tx) error {
			var admin bool
			if tx.QueryRow(r.Context(), `SELECT administrator FROM auth_users WHERE id=$1 FOR SHARE`, user).Scan(&admin) != nil || !admin {
				return access.ErrForbidden
			}
			if err := a.CheckTx(r.Context(), tx, access.Principal{UserID: user}, r.PathValue("workspace"), "", "read"); err != nil {
				return err
			}
			tag, err := tx.Exec(r.Context(), `INSERT INTO server_model_workspace_grants SELECT $1,$2,$3,1 WHERE $4::bigint=0 OR EXISTS(SELECT 1 FROM server_model_workspace_grants WHERE workspace_id=$1 AND connection_id=$2) ON CONFLICT(workspace_id,connection_id) DO UPDATE SET allowed=excluded.allowed,version=server_model_workspace_grants.version+1 WHERE server_model_workspace_grants.version=$4`, r.PathValue("workspace"), r.PathValue("connection"), req.Allowed, version)
			if err == nil && tag.RowsAffected() != 1 {
				return ErrUnavailable
			}
			return err
		})
		if err != nil {
			status := 409
			if err == access.ErrForbidden {
				status = 403
			}
			http.Error(w, "assignment_failed", status)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"version": strconv.FormatInt(version+1, 10)})
	})
}

func sortConnections(values []Connection) {
	slices.SortFunc(values, func(a, b Connection) int { return strings.Compare(a.ID, b.ID) })
}
