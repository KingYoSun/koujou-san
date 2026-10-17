ALTER TABLE access_operations DROP CONSTRAINT access_operations_purpose_check;
ALTER TABLE access_operations ADD CHECK(purpose IN ('read','edit','price-edit','triage-edit','triage-manage','hopper-edit','hopper-manage','selection-edit'));
CREATE INDEX usage_owner_groups ON usage_entries(workspace_id,attribution_kind,attribution_id,kind,coverage_group,position DESC);

CREATE INDEX search_relevant_changes ON shared_changes(workspace_id,sequence) WHERE resource_id LIKE 'context:%' OR resource_id LIKE 'membership:%' OR resource_id LIKE 'relation:%';

CREATE TABLE client_operation_records (
 workspace_id text NOT NULL,id text NOT NULL,version text NOT NULL,kind text NOT NULL,root_id text NOT NULL,
 position bigint NOT NULL,payload jsonb NOT NULL,refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX client_operation_history ON client_operation_records(workspace_id,kind,position,id);
CREATE INDEX client_operation_root_history ON client_operation_records(workspace_id,root_id,position,id);
CREATE TABLE client_operation_heads (
 workspace_id text NOT NULL,id text NOT NULL,kind text NOT NULL,state text NOT NULL,
 owner_user text NOT NULL,client_id text NOT NULL DEFAULT '',
 read_operation text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),models integer NOT NULL DEFAULT 0,
 mcp integer NOT NULL DEFAULT 0,deadline timestamptz,
 PRIMARY KEY(workspace_id,id)
);
CREATE INDEX client_operation_waiting ON client_operation_heads(workspace_id,state,created_at,id);
CREATE INDEX client_operation_read_binding ON client_operation_heads(workspace_id,read_operation) WHERE kind='operation';
CREATE TABLE access_operation_scopes (
 workspace_id text NOT NULL,operation_id text NOT NULL,scope_id text NOT NULL,
 PRIMARY KEY(workspace_id,operation_id,scope_id),
 FOREIGN KEY(workspace_id,operation_id) REFERENCES access_operations(workspace_id,id)
);
CREATE TABLE access_operation_exclusions (
 workspace_id text NOT NULL,operation_id text NOT NULL,resource_id text NOT NULL,
 PRIMARY KEY(workspace_id,operation_id,resource_id),
 FOREIGN KEY(workspace_id,operation_id) REFERENCES access_operations(workspace_id,id)
);
