CREATE TABLE forge_oauth_apps (
 id text PRIMARY KEY,
 version bigint NOT NULL,
 spec jsonb NOT NULL,
 secret bytea NOT NULL
);
CREATE TABLE forge_oauth_sessions (
 id text PRIMARY KEY,
 state_hash text UNIQUE NOT NULL,
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 user_id text NOT NULL REFERENCES auth_users(id),
 app_id text NOT NULL REFERENCES forge_oauth_apps(id),
 app_version bigint NOT NULL,
 expires_at timestamptz NOT NULL,
 state text NOT NULL,
 verifier bytea NOT NULL,
 credential bytea,
 account jsonb,
 connection_id text,
 connection_version text NOT NULL
);
CREATE INDEX forge_oauth_session_expiry ON forge_oauth_sessions(expires_at);
CREATE TABLE server_model_workspace_grants (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 connection_id text NOT NULL,
 allowed boolean NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 PRIMARY KEY(workspace_id,connection_id)
);
