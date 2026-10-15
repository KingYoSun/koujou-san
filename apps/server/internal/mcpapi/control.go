package mcpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func decode(r *http.Request, value any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(value)
}
func (s *Service) registerControl(mux *http.ServeMux) {
	// These use the ordinary authenticated client boundary, not an MCP token or
	// operation capability. No tool can create a new operation or change rules.
	mux.HandleFunc("GET /mcp/control/workspaces/{workspace}/policy", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.controlPrincipal(w, r)
		if !ok {
			return
		}
		value, version, err := s.Access.DeliveryPolicy(r.Context(), p, r.PathValue("workspace"))
		if err != nil {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		send(w, 200, map[string]any{"value": value, "version": version})
	})
	mux.HandleFunc("POST /mcp/control/workspaces/{workspace}/policy", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.controlPrincipal(w, r)
		if !ok {
			return
		}
		var request struct {
			apicontract.Mutation
			Value access.DeliveryPolicy `json:"value"`
		}
		if decode(r, &request) != nil {
			send(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		result, err := s.Access.SetDeliveryPolicy(r.Context(), p, r.PathValue("workspace"), request.Mutation, request.Value)
		s.controlResult(w, result, err)
	})
	mux.HandleFunc("POST /mcp/control/workspaces/{workspace}/operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.controlPrincipal(w, r)
		if !ok {
			return
		}
		var request struct {
			apicontract.Mutation
			Purpose  string        `json:"purpose"`
			Manifest *contexts.Ref `json:"manifest,omitempty"`
		}
		if decode(r, &request) != nil {
			send(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		workspace, id := r.PathValue("workspace"), r.PathValue("id")
		refs := []access.VersionRef{}
		if request.Manifest != nil {
			if request.Manifest.Kind != "manifest" || request.Manifest.Revision == "head" || request.Purpose != "read" {
				send(w, 400, map[string]string{"error": "invalid_manifest"})
				return
			}
			record, err := s.Contexts.Read(r.Context(), p, workspace, *request.Manifest)
			if err != nil {
				send(w, 403, map[string]string{"error": "forbidden"})
				return
			}
			var manifest contexts.Manifest
			if json.Unmarshal(record.Value, &manifest) != nil {
				send(w, 503, map[string]string{"error": "unavailable"})
				return
			}
			for _, placement := range manifest.Placements {
				refs = append(refs, access.VersionRef{ID: resource(placement.Ref), Version: placement.Ref.Revision})
			}
		}
		result, err := s.Access.CreateOperation(r.Context(), p, workspace, id, request.Purpose, request.Manifest != nil, refs, request.Mutation)
		if err != nil || result.Error != nil {
			s.controlResult(w, result, err)
			return
		}
		send(w, 200, map[string]any{"result": result, "handle": s.Access.OperationHandle(workspace, id, p.UserID)})
	})
	mux.HandleFunc("POST /mcp/control/workspaces/{workspace}/operations/{id}/close", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.controlPrincipal(w, r)
		if !ok {
			return
		}
		var request apicontract.Mutation
		if decode(r, &request) != nil {
			send(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		result, err := s.Access.CloseOperation(r.Context(), p, r.PathValue("workspace"), r.PathValue("id"), request)
		s.controlResult(w, result, err)
	})
	mux.HandleFunc("GET /mcp/control/workspaces/{workspace}/operations/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.controlPrincipal(w, r)
		if !ok {
			return
		}
		workspace, id := r.PathValue("workspace"), r.PathValue("id")
		if s.Access.Check(r.Context(), p, workspace, "", "read") != nil {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		// Events remain inspectable by their owner after closing the capability.
		var owner string
		if s.Access.DB.QueryRow(r.Context(), "SELECT user_id FROM access_operations WHERE workspace_id=$1 AND id=$2", workspace, id).Scan(&owner) != nil || owner != p.UserID {
			send(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		after := r.URL.Query().Get("after")
		rows, err := s.Access.DB.Query(r.Context(), `SELECT id,method,result,created_at FROM access_operation_events e WHERE workspace_id=$1 AND operation_id=$2 AND ($3='' OR (created_at,id)>(SELECT created_at,id FROM access_operation_events WHERE workspace_id=$1 AND operation_id=$2 AND id=$3)) ORDER BY created_at,id LIMIT 101`, workspace, id, after)
		if err != nil {
			send(w, 503, map[string]string{"error": "unavailable"})
			return
		}
		defer rows.Close()
		type event struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Time   time.Time       `json:"time"`
		}
		items := []event{}
		for rows.Next() {
			var item event
			if rows.Scan(&item.ID, &item.Method, &item.Result, &item.Time) != nil {
				send(w, 503, map[string]string{"error": "unavailable"})
				return
			}
			items = append(items, item)
		}
		if rows.Err() != nil {
			send(w, 503, map[string]string{"error": "unavailable"})
			return
		}
		next := ""
		if len(items) > 100 {
			items = items[:100]
			next = items[99].ID
		}
		send(w, 200, map[string]any{"items": items, "next": next})
	})
}
func (s *Service) controlPrincipal(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
	id, ok := s.Access.User(w, r)
	return access.Principal{UserID: id}, ok
}
func (s *Service) controlResult(w http.ResponseWriter, result apicontract.Response[apicontract.Reference], err error) {
	if err != nil {
		send(w, 403, map[string]string{"error": "forbidden"})
		return
	}
	status := 200
	if result.Error != nil {
		status = 409
	}
	send(w, status, result)
}
