CREATE TABLE client_context_origins (
 workspace_id text NOT NULL,context_id text NOT NULL,version text NOT NULL,
 operation_id text NOT NULL,payload jsonb NOT NULL,refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,context_id,version)
);
CREATE INDEX client_context_origin_operation ON client_context_origins(workspace_id,operation_id,context_id,version);
CREATE TABLE context_generation_targets (
 workspace_id text NOT NULL,target text NOT NULL,activation_id text NOT NULL,
 PRIMARY KEY(workspace_id,target,activation_id)
);
CREATE TABLE context_generation_queue (
 workspace_id text NOT NULL,id text NOT NULL,activation_id text NOT NULL,activation_version text NOT NULL,
 run_id text NOT NULL,run_version text NOT NULL,through bigint NOT NULL,after_position bigint NOT NULL DEFAULT 0,
 operation_id text NOT NULL DEFAULT '',state text NOT NULL DEFAULT 'waiting',
 owner_user text NOT NULL,client_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id)
);
CREATE INDEX context_generation_pending ON context_generation_queue(workspace_id,owner_user,client_id,state,created_at,id);
CREATE TABLE context_generation_backfills (
 workspace_id text NOT NULL,id text NOT NULL,activation_id text NOT NULL,activation_version text NOT NULL,
 owner_user text NOT NULL,client_id text NOT NULL,from_at timestamptz NOT NULL,until_at timestamptz NOT NULL,
 after_id text NOT NULL DEFAULT '',upper_id text NOT NULL,through bigint NOT NULL,state text NOT NULL DEFAULT 'waiting',
 PRIMARY KEY(workspace_id,id)
);
CREATE INDEX context_generation_backfill_pending ON context_generation_backfills(workspace_id,owner_user,client_id,state,id);
CREATE INDEX context_generation_output_channel ON run_records(workspace_id,run_id,(payload->>'channel'),position,id) INCLUDE(version) WHERE kind='output';
