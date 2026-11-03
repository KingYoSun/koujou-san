package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/jackc/pgx/v5"
)

type Repository struct {
	ID                string `json:"id"`
	Connection        string `json:"connection"`
	ConnectionVersion string `json:"connection_version"`
	Instance          string `json:"instance"`
	FullName          string `json:"full_name"`
	RepositoryID      int64  `json:"repository_id"`
	Project           string `json:"project,omitempty"`
	Private           bool   `json:"private"`
	AutoInit          bool   `json:"auto_init"`
	State             string `json:"state"`
	Version           string `json:"version"`
}
type RepositoryWrite struct {
	apicontract.Mutation
	Connection string `json:"connection"`
	Project    string `json:"project,omitempty"`
	FullName   string `json:"full_name"`
	Private    *bool  `json:"private,omitempty"`
	AutoInit   bool   `json:"auto_init"`
}
type remoteRepository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

var repositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (s *Service) Read(ctx context.Context, p access.Principal, w, id string) (Repository, error) {
	var out Repository
	var spec []byte
	var version string
	if err := s.Access.Check(ctx, p, w, "forge-repository:"+id, "get"); err != nil {
		return out, err
	}
	err := s.Store.Database().QueryRow(ctx, `SELECT r.spec,v.version FROM forge_repositories r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id='forge-repository:'||r.id WHERE r.workspace_id=$1 AND r.id=$2`, w, id).Scan(&spec, &version)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(spec, &out) != nil {
		return out, ErrInvalid
	}
	out.Version = version
	for _, target := range []string{"forge-connection:" + out.Connection, func() string {
		if out.Project != "" {
			return "scope:" + out.Project
		}
		return ""
	}()} {
		if target != "" {
			if err = s.Access.Check(ctx, p, w, target, "get"); err != nil {
				return Repository{}, err
			}
		}
	}
	return out, nil
}
func (s *Service) Repository(ctx context.Context, w, id string) (Repository, *gitea.Client, error) {
	var out Repository
	var spec []byte
	err := s.Store.Database().QueryRow(ctx, `SELECT spec FROM forge_repositories WHERE workspace_id=$1 AND id=$2 AND state IN ('registered','created','confirmed')`, w, id).Scan(&spec)
	if err != nil {
		return out, nil, err
	}
	if json.Unmarshal(spec, &out) != nil {
		return out, nil, ErrInvalid
	}
	_, client, err := s.Connection(ctx, w, out.Connection)
	return out, client, err
}
func (s *Service) Repositories(ctx context.Context, p access.Principal, w, project, after string) (Page[Repository], error) {
	out := Page[Repository]{Items: []Repository{}}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return out, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT r.spec,v.version FROM shared_versions v JOIN forge_repositories r ON r.workspace_id=v.workspace_id AND v.resource_id='forge-repository:'||r.id WHERE v.workspace_id=$1 AND NOT v.deleted AND v.resource_id LIKE 'forge-repository:%' AND v.resource_id>'forge-repository:'||$4 AND ($5='' OR r.project_id=$5) AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id','forge-repository:'||r.id,'version',v.version),jsonb_build_object('id','forge-connection:'||r.connection_id,'version',''))||CASE WHEN r.project_id='' THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object('id','scope:'||r.project_id,'version','')) END`)+` ORDER BY v.resource_id LIMIT 51`, w, p.UserID, p.OperationID, after, project)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var spec []byte
		var value Repository
		var version string
		if err = rows.Scan(&spec, &version); err != nil {
			return out, err
		}
		if json.Unmarshal(spec, &value) != nil {
			return out, ErrInvalid
		}
		value.Version = version
		out.Items = append(out.Items, value)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.Next = out.Items[49].ID
	}
	return out, rows.Err()
}
func (s *Service) saveRepository(ctx context.Context, p access.Principal, w string, r Repository, m apicontract.Mutation, deleted bool, payload any) (apicontract.Response[apicontract.Reference], error) {
	return s.mutate(ctx, p, w, "forge-repository:"+r.ID, "forge-repository", m, deleted, payload, func(tx pgx.Tx, ref apicontract.Reference) (any, error) {
		var unregistered bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2 AND deleted)`, w, ref.ID).Scan(&unregistered); err != nil {
			return nil, err
		}
		if unregistered != deleted {
			return nil, ErrState
		}
		if r.Project != "" {
			if err := s.Access.CheckTx(ctx, tx, p, w, "scope:"+r.Project, "update"); err != nil {
				return nil, err
			}
			var kind string
			if err := tx.QueryRow(ctx, `SELECT scope_kind FROM context_scope_catalog WHERE workspace_id=$1 AND resource_id=$2`, w, "scope:"+r.Project).Scan(&kind); err != nil || kind != "project" {
				return nil, ErrInvalid
			}
		}
		if err := s.Access.CheckTx(ctx, tx, p, w, "forge-connection:"+r.Connection, "get"); err != nil {
			return nil, err
		}
		r.Version = ref.Version
		_, err := tx.Exec(ctx, `INSERT INTO forge_repositories(workspace_id,id,connection_id,project_id,full_name,state,spec) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(workspace_id,id) DO UPDATE SET project_id=excluded.project_id,state=excluded.state,spec=excluded.spec`, w, r.ID, r.Connection, r.Project, r.FullName, r.State, raw(r))
		return r, err
	})
}
func (s *Service) PutRepository(ctx context.Context, p access.Principal, w, id string, req RepositoryWrite, create bool) (apicontract.Response[apicontract.Reference], error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if id == "" || !repositoryName.MatchString(req.FullName) {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	c, client, err := s.Connection(ctx, w, req.Connection)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if err = s.Access.Check(ctx, p, w, "forge-connection:"+c.ID, "get"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	r := Repository{ID: id, Connection: c.ID, ConnectionVersion: c.Version, Instance: c.Instance, FullName: req.FullName, Project: req.Project, AutoInit: req.AutoInit, State: "registered"}
	if req.ExpectedVersion != "0" {
		current, err := s.Read(ctx, p, w, id)
		if err != nil {
			return apicontract.Response[apicontract.Reference]{}, err
		}
		if create || current.Connection != c.ID || current.FullName != req.FullName || current.RepositoryID == 0 {
			return apicontract.Response[apicontract.Reference]{}, ErrState
		}
	}
	if create {
		if !c.AllowCreate || req.Private == nil || req.ExpectedVersion != "0" {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		r.Private = *req.Private
		r.State = "intent"
	} else {
		var remote remoteRepository
		if err = client.Get(ctx, "/repos/"+req.FullName, &remote); err != nil {
			return apicontract.Response[apicontract.Reference]{}, err
		}
		if remote.ID < 1 || remote.FullName != req.FullName {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		r.RepositoryID = remote.ID
		r.Private = remote.Private
	}
	accepted, err := s.saveRepository(ctx, p, w, r, req.Mutation, false, req)
	if err != nil || accepted.Error != nil || !create {
		return accepted, err
	}
	// A durable, single-use dispatch claim survives a lost HTTP response or restart.
	// Repeated create requests can only read/reconcile this same repository intent.
	var claimed string
	err = s.Store.Database().QueryRow(ctx, `UPDATE forge_repositories r SET dispatched=true FROM shared_versions v WHERE r.workspace_id=$1 AND r.id=$2 AND r.state='intent' AND NOT r.dispatched AND v.workspace_id=r.workspace_id AND v.resource_id='forge-repository:'||r.id AND NOT v.deleted RETURNING r.id`, w, id).Scan(&claimed)
	if err == pgx.ErrNoRows {
		return accepted, nil
	}
	if err != nil {
		return accepted, err
	}
	if err = s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return accepted, err
	}
	current, client, err := s.Connection(ctx, w, c.ID)
	if err != nil || !current.AllowCreate || current.Version != c.Version {
		return accepted, ErrState
	}
	var actor struct {
		Login string `json:"login"`
	}
	if err = client.Get(ctx, "/user", &actor); err != nil {
		return accepted, err
	}
	owner, name, _ := strings.Cut(req.FullName, "/")
	path := "/orgs/" + url.PathEscape(owner) + "/repos"
	if owner == actor.Login {
		path = "/user/repos"
	}
	response, sendErr := client.Request(ctx, "POST", path, strings.NewReader(string(raw(map[string]any{"name": name, "private": r.Private, "auto_init": r.AutoInit}))))
	r.State = "unknown"
	if sendErr == nil {
		var remote remoteRepository
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&remote)
		response.Body.Close()
		if response.StatusCode == 201 && decodeErr == nil && remote.ID > 0 && remote.FullName == r.FullName && remote.Private == r.Private {
			r.RepositoryID = remote.ID
			r.State = "created"
		} else if response.StatusCode >= 400 && response.StatusCode < 500 {
			r.State = "rejected"
		}
	}
	resultMutation := apicontract.Mutation{OperationID: "forge-result:" + accepted.Data.Version, ExpectedVersion: accepted.Data.Version}
	finished, finishErr := s.saveRepository(ctx, p, w, r, resultMutation, false, r)
	if finishErr == nil && finished.Error != nil && finished.Error.Code == "version_conflict" {
		// A reply to an already-dispatched request remains historical after unregistration.
		var current string
		lookupErr := s.Store.Database().QueryRow(ctx, `SELECT v.version FROM forge_repositories r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id='forge-repository:'||r.id WHERE r.workspace_id=$1 AND r.id=$2 AND v.deleted`, w, id).Scan(&current)
		if lookupErr == nil {
			resultMutation.ExpectedVersion = current
			finished, finishErr = s.saveRepository(ctx, p, w, r, resultMutation, true, r)
		} else if lookupErr != pgx.ErrNoRows {
			return accepted, lookupErr
		}
	}
	if finishErr != nil {
		return accepted, finishErr
	}
	if finished.Error != nil {
		return finished, nil
	}
	return accepted, nil
}
func (s *Service) Unregister(ctx context.Context, p access.Principal, w, id string, m apicontract.Mutation) (apicontract.Response[apicontract.Reference], error) {
	payload := map[string]string{"id": id}
	return s.mutate(ctx, p, w, "forge-repository:"+id, "forge-repository", m, true, payload, func(tx pgx.Tx, _ apicontract.Reference) (any, error) {
		var registered bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM forge_repositories r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id='forge-repository:'||r.id WHERE r.workspace_id=$1 AND r.id=$2 AND NOT v.deleted)`, w, id).Scan(&registered); err != nil {
			return nil, err
		}
		if !registered {
			return nil, ErrState
		}
		return payload, nil
	})
}

func (s *Service) Reconcile(ctx context.Context, p access.Principal, w, id string, m apicontract.Mutation) (apicontract.Response[apicontract.Reference], error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	r, err := s.Read(ctx, p, w, id)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if r.State != "unknown" && r.State != "intent" {
		return apicontract.Response[apicontract.Reference]{}, ErrState
	}
	_, client, err := s.Connection(ctx, w, r.Connection)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	var remote remoteRepository
	if err = client.Get(ctx, "/repos/"+r.FullName, &remote); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	if remote.ID < 1 || remote.FullName != r.FullName || remote.Private != r.Private {
		return apicontract.Response[apicontract.Reference]{}, ErrState
	}
	r.RepositoryID = remote.ID
	r.State = "confirmed"
	return s.saveRepository(ctx, p, w, r, m, false, r)
}

type Owner struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func (s *Service) Owners(ctx context.Context, p access.Principal, w, id string, page int) ([]Owner, error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return nil, err
	}
	if err := s.Access.Check(ctx, p, w, "forge-connection:"+id, "get"); err != nil {
		return nil, err
	}
	if page < 1 {
		return nil, ErrInvalid
	}
	_, client, err := s.Connection(ctx, w, id)
	if err != nil {
		return nil, err
	}
	out := []Owner{}
	if page == 1 {
		var own Owner
		if err = client.Get(ctx, "/user", &own); err != nil {
			return nil, err
		}
		own.Type = "user"
		out = append(out, own)
	}
	var orgs []struct {
		Name string `json:"username"`
	}
	if err = client.Get(ctx, fmt.Sprintf("/user/orgs?limit=50&page=%d", page), &orgs); err != nil {
		return nil, err
	}
	for _, org := range orgs {
		out = append(out, Owner{Login: org.Name, Type: "organization"})
	}
	return out, nil
}

// Resolve is used only by already-authorized server integrations.
func (s *Service) Resolve(ctx context.Context, w, id string) (Repository, Connection, *gitea.Client, error) {
	r, client, err := s.Repository(ctx, w, id)
	if err != nil {
		return r, Connection{}, nil, err
	}
	c, _, err := s.Connection(ctx, w, r.Connection)
	return r, c, client, err
}
