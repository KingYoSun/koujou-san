CREATE TABLE workbench_records (
  workspace_id text NOT NULL,
  id text NOT NULL,
  version text NOT NULL,
  owner_id text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('route','return')),
  recipient text NOT NULL DEFAULT '',
  position bigint NOT NULL,
  payload jsonb NOT NULL,
  refs jsonb NOT NULL,
  PRIMARY KEY (workspace_id,id,version)
);
CREATE INDEX workbench_owner_history ON workbench_records(workspace_id,owner_id,position,id);
CREATE INDEX workbench_recipient ON workbench_records(workspace_id,recipient,owner_id,id,version);
