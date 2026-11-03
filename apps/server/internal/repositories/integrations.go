package repositories

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
)

type ObservationConfig struct {
	Target  githubobserve.Target  `json:"target"`
	Binding githubobserve.Binding `json:"binding"`
	Active  bool                  `json:"active"`
	Version string                `json:"version"`
}

func (s *Service) Observations(ctx context.Context, p access.Principal, w, id, after string) (Page[ObservationConfig], error) {
	out := Page[ObservationConfig]{Items: []ObservationConfig{}}
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return out, err
	}
	if _, err := s.Read(ctx, p, w, id); err != nil {
		return out, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT t.spec,b.expected_commit,b.required_jobs,t.active,v.version FROM github_targets t JOIN github_bindings b ON b.target_id=t.id JOIN shared_versions v ON v.workspace_id=b.workspace_id AND v.resource_id=t.id WHERE b.workspace_id=$1 AND t.spec->>'workspace_id'=$1 AND t.managed AND t.spec->>'registry_repository'=$4 AND t.id>$5 AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id',t.id,'version',v.version))`)+` ORDER BY t.id LIMIT 51`, w, p.UserID, p.OperationID, id, after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ObservationConfig
		var spec, jobs []byte
		if err = rows.Scan(&spec, &c.Binding.ExpectedCommit, &jobs, &c.Active, &c.Version); err != nil {
			return out, err
		}
		if json.Unmarshal(spec, &c.Target) != nil || json.Unmarshal(jobs, &c.Binding.RequiredJobs) != nil {
			return out, ErrInvalid
		}
		c.Binding.WorkspaceID = w
		c.Binding.TargetID = c.Target.ID
		out.Items = append(out.Items, c)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.Next = out.Items[49].Target.ID
	}
	return out, rows.Err()
}

func (s *Service) AttachObserver(observer *githubobserve.Service) {
	s.Observer = observer
	observer.SetConnectionResolver(func(ctx context.Context, t githubobserve.Target) (*gitea.Client, error) {
		repository, client, err := s.Repository(ctx, t.Workspace, t.RegistryRepository)
		if err != nil {
			return nil, err
		}
		if repository.Instance != t.Instance || repository.FullName != t.Repository || repository.RepositoryID != t.RepositoryID {
			return nil, ErrState
		}
		return client, nil
	})
	observer.AuthorizeTarget = func(ctx context.Context, p access.Principal, w string, t githubobserve.Target) error {
		if t.RegistryRepository == "" {
			return nil
		}
		if t.Workspace != w {
			return access.ErrForbidden
		}
		_, err := s.Read(ctx, p, w, t.RegistryRepository)
		return err
	}
}
func (s *Service) AttachPublication(backend *publications.Remote, service *publications.Service) {
	convert := func(w string, r Repository, c Connection) publications.Connection {
		return publications.Connection{Provider: "gitea", ID: "repository:" + r.ID, Workspace: w, Instance: r.Instance, Repository: r.FullName, RepositoryID: r.RepositoryID, ActorID: c.ActorID, Resource: publications.Ref{ID: "forge-repository:" + r.ID, Version: r.Version}, Credential: publications.Ref{ID: "forge-connection:" + c.ID, Version: c.Version}}
	}
	backend.Lookup = func(w, id string) (publications.Connection, *gitea.Client, bool) {
		if !strings.HasPrefix(id, "repository:") {
			return publications.Connection{}, nil, false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		r, c, client, err := s.Resolve(ctx, w, strings.TrimPrefix(id, "repository:"))
		if err != nil {
			return publications.Connection{}, nil, false
		}
		return convert(w, r, c), client, true
	}
	service.ListConnections = func(ctx context.Context, p access.Principal, w, after string) (any, error) {
		out := Page[publications.Connection]{Items: []publications.Connection{}}
		if !strings.HasPrefix(after, "repository:") {
			for _, c := range backend.Connections(w) {
				if c.ID > strings.TrimPrefix(after, "legacy:") {
					out.Items = append(out.Items, c)
				}
			}
			if len(out.Items) > 50 {
				out.Items = out.Items[:50]
				out.Next = "legacy:" + out.Items[49].ID
				return out, nil
			}
			if len(out.Items) > 0 {
				out.Next = "repository:"
				return out, nil
			}
		}
		repos, err := s.Repositories(ctx, p, w, "", strings.TrimPrefix(after, "repository:"))
		if err != nil {
			return nil, err
		}
		for _, r := range repos.Items {
			if r.RepositoryID > 0 {
				c, _, err := s.Connection(ctx, w, r.Connection)
				if err != nil {
					return nil, err
				}
				out.Items = append(out.Items, convert(w, r, c))
			}
		}
		if repos.Next != "" {
			out.Next = "repository:" + repos.Next
		}
		return out, nil
	}
}
