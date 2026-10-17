CREATE TABLE inspection_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 version text NOT NULL,
 owner_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('definition','adoption','launch','evidence','judgment')),
 position bigint NOT NULL,
 payload jsonb NOT NULL,
 refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX inspection_heads ON inspection_records(workspace_id,kind,id,position DESC);
CREATE INDEX inspection_history ON inspection_records(workspace_id,owner_id,position,id);
CREATE TABLE inspection_launches (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 run_id text NOT NULL,
 launch_id text NOT NULL,
 version text NOT NULL,
 PRIMARY KEY(workspace_id,run_id)
);
