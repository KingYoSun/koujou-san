CREATE TABLE publication_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 version text NOT NULL,
 owner_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('draft','request','observation')),
 position bigint NOT NULL,
 payload jsonb NOT NULL,
 refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX publication_heads ON publication_records(workspace_id,kind,id,position DESC);
CREATE INDEX publication_history ON publication_records(workspace_id,owner_id,position,id);
CREATE TABLE publication_queue (
 workspace_id text NOT NULL,
 id text NOT NULL,
 run_id text NOT NULL,
 state text NOT NULL,
 due_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id),
 UNIQUE(workspace_id,run_id)
);
CREATE INDEX publication_due ON publication_queue(due_at,workspace_id,id)
 WHERE state IN ('accepted','sending','unknown','finishing');
