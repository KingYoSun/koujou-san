package githubobserve

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

func encode(value any) []byte { raw, _ := json.Marshal(value); return raw }

func (s *Service) SetConnectionResolver(resolve func(context.Context, Target) (*gitea.Client, error)) {
	s.remote.Lookup = resolve
}
func (r *Remote) giteaClient(ctx context.Context, t Target) (*gitea.Client, error) {
	if t.RegistryRepository != "" {
		if r.Lookup == nil {
			return nil, errors.New("not_configured")
		}
		return r.Lookup(ctx, t)
	}
	client := r.config.clients[t.Instance]
	if client == nil {
		return nil, errors.New("not_configured")
	}
	return client, nil
}
func (s *Service) PutManagedTarget(ctx context.Context, p access.Principal, w string, t Target, b Binding, m apicontract.Mutation, active bool) (apicontract.Response[apicontract.Reference], error) {
	if err := s.access.Check(ctx, p, w, "", "manage"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	client, err := s.remote.giteaClient(ctx, t)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	config := Config{clients: map[string]*gitea.Client{t.Instance: client}}
	if t.Workspace != w || t.RegistryRepository == "" || config.validateGitea(t, map[string]bool{}, map[string]bool{}) != nil || (b.ExpectedCommit != "" && !shaPattern.MatchString(b.ExpectedCommit)) {
		return apicontract.Response[apicontract.Reference]{}, gitea.ErrConfig
	}
	if active {
		if err = client.Repository(ctx, t.Repository, t.RepositoryID); err != nil {
			return apicontract.Response[apicontract.Reference]{}, err
		}
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if s.access.CheckTx(ctx, tx, p, w, "", "manage") != nil || s.access.CheckTx(ctx, tx, p, w, "forge-repository:"+t.RegistryRepository, "get") != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	value := struct {
		Target  Target  `json:"target"`
		Binding Binding `json:"binding"`
		Active  bool    `json:"active"`
	}{t, b, active}
	return s.store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: t.ID, Kind: "forge-observation-target", Mutation: m, Payload: encode(value)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		tag, err := tx.Exec(ctx, `INSERT INTO github_targets(id,spec,installation_id,repository_id,managed,active) VALUES($1,$2,0,$3,true,$4) ON CONFLICT(id) DO UPDATE SET active=excluded.active,due_at=now() WHERE github_targets.managed AND github_targets.spec=excluded.spec`, t.ID, encode(t), t.RepositoryID, active)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, gitea.ErrConfig
		}
		_, err = tx.Exec(ctx, `INSERT INTO github_bindings VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,target_id) DO UPDATE SET expected_commit=excluded.expected_commit,required_jobs=excluded.required_jobs`, w, t.ID, b.ExpectedCommit, encode(b.RequiredJobs))
		if err != nil {
			return nil, err
		}
		if m.ExpectedVersion == "0" {
			if err = s.access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: t.ID}, nil); err != nil {
				return nil, err
			}
		}
		return encode(value), nil
	})
}
