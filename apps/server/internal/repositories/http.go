package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
)

func (s *Service) Register(mux *http.ServeMux) {
	s.registerOAuth(mux)
	base := "/forge/workspaces/{workspace}"
	route := func(method, path string, f func(context.Context, access.Principal, string, *http.Request) (any, error)) {
		mux.HandleFunc(method+" "+base+path, func(w http.ResponseWriter, r *http.Request) {
			user, ok := s.Access.User(w, r)
			if !ok {
				return
			}
			value, err := f(r.Context(), access.Principal{UserID: user}, r.PathValue("workspace"), r)
			send(w, value, err)
		})
	}
	body := func(r *http.Request, value any) error {
		d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if d.Decode(value) != nil {
			return ErrInvalid
		}
		return nil
	}
	route("GET", "/connections", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		return s.Connections(ctx, p, w, r.URL.Query().Get("after"))
	})
	route("GET", "/connections/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		if err := s.Access.Check(ctx, p, w, "forge-connection:"+r.PathValue("id"), "get"); err != nil {
			return nil, err
		}
		c, _, err := s.connectionRecord(ctx, w, r.PathValue("id"))
		return c, err
	})
	route("POST", "/connections/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req ConnectionWrite
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.PutConnection(ctx, p, w, r.PathValue("id"), req)
	})
	route("GET", "/connections/{id}/owners", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		return s.Owners(ctx, p, w, r.PathValue("id"), page)
	})
	route("GET", "/repositories", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		return s.Repositories(ctx, p, w, r.URL.Query().Get("project"), r.URL.Query().Get("after"))
	})
	route("GET", "/repositories/{id}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		return s.Read(ctx, p, w, r.PathValue("id"))
	})
	for _, action := range []string{"register", "create"} {
		route("POST", "/repositories/{id}/"+action, func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
			var req RepositoryWrite
			if err := body(r, &req); err != nil {
				return nil, err
			}
			return s.PutRepository(ctx, p, w, r.PathValue("id"), req, action == "create")
		})
	}
	route("POST", "/repositories/{id}/delete", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req apicontract.Mutation
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.DeleteRejected(ctx, p, w, r.PathValue("id"), req)
	})
	route("POST", "/repositories/{id}/reconcile", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req apicontract.Mutation
		if err := body(r, &req); err != nil {
			return nil, err
		}
		return s.Reconcile(ctx, p, w, r.PathValue("id"), req)
	})
	route("POST", "/repositories/{id}/observations/{target}", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		var req struct {
			apicontract.Mutation
			Kind           string   `json:"kind"`
			ExternalID     string   `json:"external_id"`
			Workflow       string   `json:"workflow"`
			Event          string   `json:"event"`
			ExpectedCommit string   `json:"expected_commit"`
			RequiredJobs   []string `json:"required_jobs"`
			Active         *bool    `json:"active,omitempty"`
		}
		if err := body(r, &req); err != nil {
			return nil, err
		}
		if s.Observer == nil {
			return nil, ErrState
		}
		repository, err := s.Read(ctx, p, w, r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		if repository.RepositoryID < 1 {
			return nil, ErrState
		}
		target := githubobserve.Target{ID: "gitea:" + r.PathValue("target"), Workspace: w, RegistryRepository: repository.ID, Provider: "gitea", Instance: repository.Instance, Repository: repository.FullName, RepositoryID: repository.RepositoryID, Kind: req.Kind, ExternalID: req.ExternalID, Workflow: req.Workflow, Event: req.Event}
		return s.Observer.PutManagedTarget(ctx, p, w, target, githubobserve.Binding{WorkspaceID: w, TargetID: target.ID, ExpectedCommit: req.ExpectedCommit, RequiredJobs: req.RequiredJobs}, req.Mutation, req.Active == nil || *req.Active)
	})
	route("GET", "/repositories/{id}/observations", func(ctx context.Context, p access.Principal, w string, r *http.Request) (any, error) {
		return s.Observations(ctx, p, w, r.PathValue("id"), r.URL.Query().Get("after"))
	})
}
