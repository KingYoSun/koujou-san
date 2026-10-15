CREATE TABLE context_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 resource_id text NOT NULL,
 kind text NOT NULL,
 revision text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(workspace_id,resource_id,revision)
);
CREATE INDEX context_record_history ON context_records(workspace_id,resource_id);
