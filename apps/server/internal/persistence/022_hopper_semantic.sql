CREATE TABLE hopper_model_attempts (
 workspace_id text NOT NULL,evaluation_id text NOT NULL,number integer NOT NULL,
 resource_id text NOT NULL,input_id text NOT NULL,input_version text NOT NULL,
 job_id text NOT NULL,lease text NOT NULL,actor text NOT NULL,
 response_version text,state text NOT NULL DEFAULT 'reserved',
 PRIMARY KEY(workspace_id,evaluation_id,number),UNIQUE(workspace_id,resource_id)
);
CREATE TABLE hopper_evidence_watches (
 workspace_id text NOT NULL,resource_id text NOT NULL,evaluation_id text NOT NULL,
 definition_id text NOT NULL,definition_version text NOT NULL,root_id text NOT NULL,
 target_id text NOT NULL,dry_run boolean NOT NULL,source_position bigint NOT NULL REFERENCES hopper_sources(position),
 PRIMARY KEY(workspace_id,resource_id,definition_id,root_id,target_id,dry_run)
);
CREATE INDEX hopper_evidence_source ON hopper_evidence_watches(workspace_id,evaluation_id);
CREATE INDEX hopper_evaluation_subject ON hopper_records(workspace_id,(payload#>>'{source,target,id}'),(payload#>>'{definition,id}'),position DESC) WHERE kind='evaluation';
CREATE INDEX hopper_model_input ON hopper_model_attempts(workspace_id,input_id,input_version,evaluation_id);
CREATE INDEX hopper_model_input_root ON hopper_records(workspace_id,(payload#>>'{source,root}'),id,version) WHERE kind='model-input';
