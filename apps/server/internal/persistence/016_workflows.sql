CREATE TABLE workflow_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 owner_id text NOT NULL,
 version text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('run-definition','workflow-definition','validation','adoption','execution','task','event')),
 position bigint NOT NULL,
 payload jsonb NOT NULL,
 refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX workflow_record_page ON workflow_records(workspace_id,kind,id,position DESC);
CREATE INDEX workflow_history ON workflow_records(workspace_id,owner_id,position,id);

CREATE TABLE workflow_ready (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 execution_id text NOT NULL,
 state text NOT NULL,
 due_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,execution_id)
);
CREATE INDEX workflow_due ON workflow_ready(due_at,workspace_id,execution_id) WHERE state IN ('running','waiting');

CREATE TABLE workflow_tasks (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 execution_id text NOT NULL,
 node_id text NOT NULL,
 attempt integer NOT NULL,
 run_id text NOT NULL DEFAULT '',
 current boolean NOT NULL,
 state text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(workspace_id,execution_id,node_id,attempt)
);
CREATE UNIQUE INDEX workflow_current_node ON workflow_tasks(workspace_id,execution_id,node_id) WHERE current;
CREATE INDEX workflow_task_state ON workflow_tasks(workspace_id,execution_id,state,node_id) WHERE current;
CREATE UNIQUE INDEX workflow_run ON workflow_tasks(workspace_id,run_id) WHERE run_id<>'';
CREATE INDEX usage_entry_run ON usage_entries(workspace_id,attribution_kind,attribution_id,kind,projection_key,position DESC);

-- Existing capability snapshots gain the same reference policy as new contacts.
INSERT INTO access_resources(workspace_id,id,restricted)
 SELECT workspace_id,'runner:'||device_id,false FROM run_runners
 ON CONFLICT DO NOTHING;
