CREATE TABLE context_scope_catalog (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 resource_id text NOT NULL,
 revision text NOT NULL,
 scope_kind text NOT NULL,
 sort_name text COLLATE "C" NOT NULL,
 PRIMARY KEY(workspace_id,resource_id)
);
INSERT INTO context_scope_catalog
 SELECT r.workspace_id,r.resource_id,r.revision,r.payload->>'kind',lower(r.payload->>'name')
 FROM context_records r JOIN shared_versions v ON v.workspace_id=r.workspace_id AND v.resource_id=r.resource_id AND v.version=r.revision
 WHERE r.kind='scope' AND NOT v.deleted;
CREATE INDEX context_scope_kind_name ON context_scope_catalog(workspace_id,scope_kind,sort_name,resource_id);

CREATE TABLE forge_connections (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 spec jsonb NOT NULL,
 credential bytea NOT NULL,
 PRIMARY KEY(workspace_id,id)
);
CREATE TABLE forge_repositories (
 workspace_id text NOT NULL,
 id text NOT NULL,
 connection_id text NOT NULL,
 project_id text NOT NULL DEFAULT '',
 full_name text NOT NULL,
 state text NOT NULL,
 dispatched boolean NOT NULL DEFAULT false,
 spec jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id),
 FOREIGN KEY(workspace_id,connection_id) REFERENCES forge_connections(workspace_id,id)
);
CREATE INDEX forge_project_repositories ON forge_repositories(workspace_id,project_id,id);
ALTER TABLE github_targets ADD COLUMN managed boolean NOT NULL DEFAULT false;
CREATE INDEX github_managed_repository ON github_targets((spec->>'workspace_id'),(spec->>'registry_repository'),id) WHERE managed;
