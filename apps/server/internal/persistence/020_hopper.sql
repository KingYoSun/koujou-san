ALTER TABLE access_operations DROP CONSTRAINT access_operations_purpose_check;
ALTER TABLE access_operations ADD CHECK(purpose IN ('read','edit','price-edit','triage-edit','triage-manage','hopper-edit','hopper-manage'));

CREATE TABLE hopper_records (
 workspace_id text NOT NULL,id text NOT NULL,version text NOT NULL,kind text NOT NULL,
 position bigint NOT NULL,payload jsonb NOT NULL,refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX hopper_heads ON hopper_records(workspace_id,kind,position,id);
CREATE INDEX hopper_chain_history ON hopper_records(workspace_id,(payload->>'root'),position) WHERE kind='evaluation';
CREATE TABLE hopper_targets (
 workspace_id text NOT NULL,kind text NOT NULL,target text NOT NULL,definition_id text NOT NULL,
 PRIMARY KEY(workspace_id,kind,target,definition_id)
);
CREATE TABLE hopper_sources (
 position bigserial PRIMARY KEY,workspace_id text NOT NULL,event_id text NOT NULL,kind text NOT NULL,
 target_id text NOT NULL,target_version text NOT NULL,payload jsonb NOT NULL,
 UNIQUE(workspace_id,event_id,target_id,target_version)
);
CREATE INDEX hopper_source_page ON hopper_sources(workspace_id,kind,target_id,position);
CREATE INDEX hopper_source_kind_page ON hopper_sources(workspace_id,kind,position);
CREATE TABLE hopper_source_keys (
 workspace_id text NOT NULL,kind text NOT NULL,key text NOT NULL,
 position bigint NOT NULL REFERENCES hopper_sources(position),
 PRIMARY KEY(workspace_id,kind,key,position)
);
CREATE TABLE hopper_chains (
 workspace_id text NOT NULL,id text NOT NULL,started_at timestamptz NOT NULL DEFAULT now(),
 evaluations integer NOT NULL DEFAULT 0,publications integer NOT NULL DEFAULT 0,
 stopped boolean NOT NULL DEFAULT false,bounds jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id)
);
CREATE TABLE hopper_lineage (
 workspace_id text NOT NULL,resource_id text NOT NULL,root text NOT NULL,depth integer NOT NULL,
 evaluation_id text NOT NULL,evaluation_version text NOT NULL,PRIMARY KEY(workspace_id,resource_id)
);
CREATE TABLE hopper_problems (
 workspace_id text NOT NULL,key text NOT NULL,destination jsonb NOT NULL,at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,key)
);
CREATE INDEX hopper_lineage_root ON hopper_lineage(workspace_id,root,resource_id);
CREATE TABLE hopper_dispatch (
 workspace_id text NOT NULL,run_id text NOT NULL,evaluation_id text NOT NULL,evaluation_version text NOT NULL,
 definition jsonb NOT NULL,controller text NOT NULL,preferred_device text NOT NULL,repository jsonb,required jsonb NOT NULL,
 state text NOT NULL DEFAULT 'waiting',reason text NOT NULL DEFAULT '',due_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,run_id)
);
CREATE INDEX hopper_dispatch_due ON hopper_dispatch(due_at,workspace_id,run_id) WHERE state='waiting';
