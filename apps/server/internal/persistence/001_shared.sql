CREATE TABLE shared_streams (
    workspace_id text PRIMARY KEY,
    origin text NOT NULL,
    position text NOT NULL,
    sequence bigint NOT NULL DEFAULT 0 CHECK (sequence >= 0)
);

CREATE TABLE shared_versions (
    workspace_id text NOT NULL REFERENCES shared_streams(workspace_id),
    resource_id text NOT NULL,
    version text NOT NULL,
    deleted boolean NOT NULL,
    PRIMARY KEY (workspace_id, resource_id)
);

CREATE TABLE shared_changes (
    workspace_id text NOT NULL REFERENCES shared_streams(workspace_id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    position text NOT NULL,
    resource_id text NOT NULL,
    version text NOT NULL,
    kind text NOT NULL,
    deleted boolean NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (workspace_id, sequence),
    UNIQUE (workspace_id, position)
);

CREATE TABLE shared_receipts (
    workspace_id text NOT NULL REFERENCES shared_streams(workspace_id),
    actor_id text NOT NULL,
    operation_id text NOT NULL,
    digest text NOT NULL,
    result jsonb NOT NULL,
    PRIMARY KEY (workspace_id, actor_id, operation_id)
);
