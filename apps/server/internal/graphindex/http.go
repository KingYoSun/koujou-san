package graphindex

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /graph/workspaces/{workspace}/query", s.queryHTTP)
	mux.HandleFunc("POST /graph/workspaces/{workspace}/resolve", s.resolveHTTP)
	mux.HandleFunc("PUT /graph/workspaces/{workspace}/relations/{id}", s.relationHTTP)
}
func (s *Service) principal(w http.ResponseWriter, r *http.Request) (access.Principal, bool) {
	id, ok := s.Access.User(w, r)
	return access.Principal{UserID: id}, ok
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func graphStatus(result apicontract.Response[Result]) int {
	if result.Error == nil {
		return 200
	}
	switch result.Error.Code {
	case "forbidden":
		return 403
	case "invalid_request":
		return 400
	default:
		return 503
	}
}
func (s *Service) queryHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request Request
	if decode(r, &request) != nil {
		send(w, 400, failed(workspace, "invalid_request", "invalid_graph"))
		return
	}
	result, _ := s.Query(r.Context(), p, workspace, request)
	send(w, graphStatus(result), result)
}

type ResolveRequest struct {
	Graph    Request                 `json:"graph"`
	Resolve  contexts.ResolveRequest `json:"resolve"`
	ID       string                  `json:"id,omitempty"`
	Mutation *apicontract.Mutation   `json:"mutation,omitempty"`
}

func (s *Service) resolveHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request ResolveRequest
	if decode(r, &request) != nil {
		send(w, 400, failed(workspace, "invalid_request", "invalid_graph"))
		return
	}
	found, _ := s.Query(r.Context(), p, workspace, request.Graph)
	if found.Error != nil || found.Data == nil || found.Meta.Completeness.State != "complete" {
		send(w, 200, apicontract.Response[contexts.Resolution]{Data: &contexts.Resolution{Missing: []contexts.Gap{{Code: "incomplete_candidates", Source: "arcadedb:" + found.Meta.Completeness.Reason}}}, Meta: found.Meta})
		return
	}
	candidates := []contexts.Ref{}
	for _, item := range found.Data.Items {
		if item.Node != nil && item.Node.Revision != "" && unitKind(item.Node.Kind) {
			candidates = append(candidates, item.Node.Ref())
		}
	}
	prepared, gap := contexts.CandidateInput(request.Resolve, candidates, found.Data.Evidence, "outside_graph")
	if gap != nil {
		send(w, 200, apicontract.Response[contexts.Resolution]{Data: &contexts.Resolution{Missing: []contexts.Gap{*gap}}, Meta: found.Meta})
		return
	}
	request.Resolve = prepared
	if request.Mutation == nil {
		resolution := s.Contexts.Preview(r.Context(), p, workspace, request.Resolve)
		send(w, 200, apicontract.Response[contexts.Resolution]{Data: &resolution, Meta: found.Meta})
		return
	}
	result, err := s.Contexts.Put(r.Context(), p, workspace, contexts.Identity{Kind: "manifest", ID: request.ID}, contexts.Write{Mutation: *request.Mutation, Value: encode(request.Resolve)})
	if err != nil {
		code, status := "unavailable", 503
		if errors.Is(err, access.ErrForbidden) {
			code, status = "forbidden", 403
		}
		send(w, status, failed(workspace, code, "resolution_unavailable"))
		return
	}
	status := 200
	if result.Error != nil {
		status = 409
	}
	send(w, status, result)
}

func (s *Service) relationHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	workspace := r.PathValue("workspace")
	var request RelationWrite
	if decode(r, &request) != nil {
		send(w, 400, failed(workspace, "invalid_request", "invalid_relation"))
		return
	}
	result, err := s.PutRelation(r.Context(), p, workspace, r.PathValue("id"), request)
	if err != nil {
		code, status := "unavailable", 503
		if errors.Is(err, access.ErrForbidden) {
			code, status = "forbidden", 403
		} else if errors.Is(err, errInput) {
			code, status = "invalid_request", 400
		}
		send(w, status, failed(workspace, code, "relation_unavailable"))
		return
	}
	status := 200
	if result.Error != nil {
		status = 409
	}
	send(w, status, result)
}
