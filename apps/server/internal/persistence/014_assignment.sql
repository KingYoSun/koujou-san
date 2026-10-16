CREATE TABLE run_runners (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 device_id text NOT NULL,
 version text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(workspace_id,device_id)
);
