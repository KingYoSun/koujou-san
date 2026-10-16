CREATE TABLE run_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 run_id text NOT NULL,
 id text NOT NULL,
 version text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('run','input','pause','output','event','result','link')),
 position bigint NOT NULL,
 payload jsonb NOT NULL,
 refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX run_record_page ON run_records(workspace_id,run_id,kind,position,id);
CREATE INDEX run_record_list ON run_records(workspace_id,kind,id,position DESC);
