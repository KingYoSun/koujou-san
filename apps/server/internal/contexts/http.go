package contexts

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

func EnvironmentLimits() (Limits, error) {
	limits := DefaultLimits()
	for name, target := range map[string]*int{"KOUJOU_CONTEXT_MAX_BYTES": &limits.Bytes, "KOUJOU_CONTEXT_MAX_PLACEMENTS": &limits.Placements} {
		if raw, ok := os.LookupEnv(name); ok {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				return Limits{}, errors.New("invalid Context limits")
			}
			*target = n
		}
	}
	return limits, nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func metadata(w string, position *string, state, reason string) apicontract.Meta {
	return apicontract.Meta{WorkspaceID: w, Position: position, Completeness: apicontract.Completeness{State: state, Reason: reason}}
}
func sendError(w http.ResponseWriter, workspace string, err error) {
	status, code, reason := 503, "unavailable", "storage_unavailable"
	var f *failure
	if errors.As(err, &f) {
		status, code, reason = 400, "invalid_request", f.gap.Code
		if f.gap.Code == "forbidden" {
			status, code = 403, "forbidden"
		}
	}
	writeJSON(w, status, apicontract.Response[any]{Error: &apicontract.Problem{Code: code}, Meta: metadata(workspace, nil, "pending", reason)})
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /contexts/workspaces/{workspace}/resolve", s.previewHTTP)
	mux.HandleFunc("GET /contexts/workspaces/{workspace}/{kind}/{id}", s.readHTTP)
	mux.HandleFunc("POST /contexts/workspaces/{workspace}/{kind}/{id}", s.writeHTTP)
}
func (s *Service) principal(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
	id, ok := s.Access.User(w, r)
	return access.Principal{UserID: id}, ok
}
func (s *Service) readHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	revision := r.URL.Query().Get("revision")
	if revision == "" {
		revision = "head"
	}
	record, err := s.Read(r.Context(), p, workspace, Ref{r.PathValue("kind"), r.PathValue("id"), revision})
	if err != nil {
		sendError(w, workspace, err)
		return
	}
	writeJSON(w, 200, apicontract.Response[Record]{Data: &record, Meta: metadata(workspace, &record.Position, "complete", "")})
}
func (s *Service) writeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request Write
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		sendError(w, workspace, invalid("invalid_request", nil))
		return
	}
	result, err := s.Put(r.Context(), p, workspace, Identity{r.PathValue("kind"), r.PathValue("id")}, request)
	if err != nil {
		sendError(w, workspace, err)
		return
	}
	status := 200
	if result.Error != nil {
		switch result.Error.Code {
		case "forbidden":
			status = 403
		case "invalid_request":
			status = 400
		case "unavailable":
			status = 503
		default:
			status = 409
		}
	}
	writeJSON(w, status, result)
}
func (s *Service) previewHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request ResolveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		sendError(w, workspace, invalid("invalid_request", nil))
		return
	}
	if err := s.Access.Check(r.Context(), p, workspace, "", "read"); err != nil {
		sendError(w, workspace, invalid("forbidden", nil))
		return
	}
	result := s.Preview(r.Context(), p, workspace, request)
	var position string
	if err := s.Store.Database().QueryRow(r.Context(), "SELECT position FROM shared_streams WHERE workspace_id=$1", workspace).Scan(&position); err != nil {
		sendError(w, workspace, err)
		return
	}
	state, reason := "complete", ""
	if !result.Ready {
		state, reason = "pending", result.Missing[0].Code
	}
	writeJSON(w, 200, apicontract.Response[Resolution]{Data: &result, Meta: metadata(workspace, &position, state, reason)})
}
