package sharedsync

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, workspace string, err error) {
	status, code, reason := 503, "unavailable", "storage_unavailable"
	switch {
	case errors.Is(err, errInput):
		status, code, reason = 400, "invalid_request", "invalid_request"
	case errors.Is(err, access.ErrForbidden):
		status, code, reason = 403, "forbidden", "not_accessed"
	case errors.Is(err, errCursor):
		status, code, reason = 200, "invalid_request", "invalid_cursor"
	case errors.Is(err, errHistory):
		status, code, reason = 200, "unavailable", "history_unavailable"
	case errors.Is(err, errSequence):
		status, code, reason = 409, "operation_conflict", "report_sequence_conflict"
	}
	send(w, status, apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: reason}}})
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
	mux.HandleFunc("POST /sync/workspaces/{workspace}/pull", func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.Access.User(w, r)
		if !ok {
			return
		}
		workspace := r.PathValue("workspace")
		var request Request
		if !decode(w, r, &request) {
			return
		}
		value, err := s.Pull(r.Context(), access.Principal{UserID: id}, workspace, request)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		send(w, 200, value)
	})
	mux.HandleFunc("POST /sync/workspaces/{workspace}/reports", func(w http.ResponseWriter, r *http.Request) {
		workspace := r.PathValue("workspace")
		p, err := s.Access.DevicePrincipal(r.Context(), workspace, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			fail(w, workspace, err)
			return
		}
		var event ReportInput
		if !decode(w, r, &event) {
			return
		}
		value, err := s.Report(r.Context(), p, workspace, event)
		if err != nil {
			fail(w, workspace, err)
			return
		}
		status := 200
		if value.Error != nil {
			switch value.Error.Code {
			case "forbidden":
				status = 403
			case "unavailable":
				status = 503
			default:
				status = 409
			}
		}
		send(w, status, value)
	})
}
