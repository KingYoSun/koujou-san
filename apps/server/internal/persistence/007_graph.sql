CREATE TABLE graph_relations (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 revision text NOT NULL,
 payload jsonb NOT NULL,
 stream_sequence bigint NOT NULL,
 PRIMARY KEY(workspace_id,id,revision)
);
CREATE INDEX graph_relation_history ON graph_relations(workspace_id,id,stream_sequence DESC);

CREATE INDEX graph_run_source ON graph_relations(workspace_id,(payload->'from'->>'id'),(payload->'from'->>'revision')) WHERE payload->'from'->>'kind'='run';
CREATE INDEX graph_run_target ON graph_relations(workspace_id,(payload->'to'->>'id'),(payload->'to'->>'revision')) WHERE payload->'to'->>'kind'='run';
