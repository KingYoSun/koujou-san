CREATE TABLE github_targets (
 id text PRIMARY KEY,
 spec jsonb NOT NULL,
 installation_id bigint NOT NULL,
 repository_id bigint NOT NULL,
 active boolean NOT NULL DEFAULT true,
 due_at timestamptz NOT NULL DEFAULT now(),
 checked_at timestamptz,
 observed_at timestamptz,
 reason text NOT NULL DEFAULT 'not_fetched',
 snapshot jsonb,
 version bigint NOT NULL DEFAULT 0
);
CREATE INDEX github_targets_due ON github_targets(due_at,id) WHERE active;
CREATE INDEX github_targets_repository ON github_targets(installation_id,repository_id) WHERE active;
CREATE TABLE github_bindings (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 target_id text NOT NULL REFERENCES github_targets(id),
 expected_commit text NOT NULL,
 required_jobs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,target_id)
);
CREATE TABLE github_observations (
 target_id text NOT NULL REFERENCES github_targets(id),
 version bigint NOT NULL,
 observed_at timestamptz NOT NULL,
 snapshot jsonb NOT NULL,
 PRIMARY KEY(target_id,version)
);
