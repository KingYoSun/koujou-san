ALTER TABLE access_operations DROP CONSTRAINT access_operations_purpose_check;
ALTER TABLE access_operations ADD CHECK(purpose IN ('read','edit','price-edit'));

CREATE TABLE usage_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 version text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('usage','price','evaluation')),
 position bigint NOT NULL,
 coverage_group text NOT NULL DEFAULT '',
 payload jsonb NOT NULL,
 dependencies jsonb NOT NULL DEFAULT '[]',
 PRIMARY KEY(workspace_id,id,version),
 UNIQUE(workspace_id,position)
);
CREATE INDEX usage_history ON usage_records(workspace_id,kind,id,position DESC);
CREATE INDEX usage_group_records ON usage_records(workspace_id,coverage_group,id,position DESC) WHERE kind='usage';
CREATE TABLE usage_events (
 workspace_id text NOT NULL,
 source text NOT NULL,
 event_id text NOT NULL,
 resource_id text NOT NULL,
 sequence bigint NOT NULL,
 PRIMARY KEY(workspace_id,source,event_id)
);
CREATE TABLE usage_group_updates (
 workspace_id text NOT NULL,
 coverage_group text NOT NULL,
 position bigint NOT NULL,
 affected_from timestamptz NOT NULL,
 affected_until timestamptz NOT NULL,
 resource_id text NOT NULL,
 version text NOT NULL,
 PRIMARY KEY(workspace_id,coverage_group,position)
);
CREATE INDEX usage_updates_position ON usage_group_updates(workspace_id,position);
CREATE TABLE usage_entries (
 workspace_id text NOT NULL,
 projection_key text NOT NULL,
 position bigint NOT NULL,
 coverage_group text NOT NULL,
 kind text NOT NULL,
 currency text NOT NULL,
 at timestamptz NOT NULL,
 attribution_kind text NOT NULL,
 attribution_id text NOT NULL,
 workflow text NOT NULL,
 scopes text[] NOT NULL,
 model text NOT NULL,
 amount numeric,
 state text NOT NULL,
 refs jsonb NOT NULL,
 payload jsonb NOT NULL,
 quantities jsonb NOT NULL,
 PRIMARY KEY(workspace_id,projection_key,position)
);
CREATE INDEX usage_entry_period ON usage_entries(workspace_id,kind,at,projection_key,position DESC);
CREATE INDEX usage_entry_latest ON usage_entries(workspace_id,kind,projection_key,position DESC);
CREATE INDEX usage_entry_scopes ON usage_entries USING gin(scopes);
