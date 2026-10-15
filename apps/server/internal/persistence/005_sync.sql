ALTER TABLE context_records ADD COLUMN stream_sequence bigint;
UPDATE context_records r SET stream_sequence=c.sequence FROM shared_changes c
 WHERE r.workspace_id=c.workspace_id AND r.resource_id=c.resource_id AND r.revision=c.version;
ALTER TABLE context_records ALTER COLUMN stream_sequence SET NOT NULL;
CREATE INDEX context_snapshot_versions ON context_records(workspace_id,resource_id,stream_sequence DESC);

CREATE TABLE sync_reports (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 event_id text NOT NULL,
 topic_id text NOT NULL,
 target_id text NOT NULL,
 device_id text NOT NULL,
 source_sequence bigint NOT NULL CHECK(source_sequence>0),
 kind text NOT NULL CHECK(kind IN ('input_observed','pause_observed','output_observed','completion_observed')),
 artifact_id text NOT NULL,
 artifact_revision text NOT NULL,
 PRIMARY KEY(workspace_id,event_id),
 UNIQUE(workspace_id,topic_id,source_sequence)
);
CREATE TABLE sync_report_versions (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 topic_id text NOT NULL,
 revision text NOT NULL,
 stream_sequence bigint NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(workspace_id,topic_id,revision)
);
CREATE INDEX report_snapshot_versions ON sync_report_versions(workspace_id,topic_id,stream_sequence DESC);
