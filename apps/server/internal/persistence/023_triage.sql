ALTER TABLE automation_records DROP CONSTRAINT automation_records_kind_check;
ALTER TABLE automation_records ADD CHECK(kind IN ('policy','activation','control','priority','application','issue','model-input','model-attempt','sync','batch'));
CREATE TABLE triage_targets (
 workspace_id text NOT NULL,scope_id text NOT NULL,scope_version text NOT NULL,
 info_id text NOT NULL DEFAULT '',info_version text NOT NULL DEFAULT '',
 latest_job text NOT NULL DEFAULT '',basis text NOT NULL DEFAULT '',
 deadline timestamptz,next_due timestamptz,received_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,scope_id)
);
CREATE INDEX triage_deadlines ON triage_targets(next_due,workspace_id,scope_id) WHERE next_due IS NOT NULL;
CREATE TABLE triage_watches (
 workspace_id text NOT NULL,resource_id text NOT NULL,scope_id text NOT NULL,
 PRIMARY KEY(workspace_id,resource_id,scope_id)
);
CREATE INDEX triage_watch_target ON triage_watches(workspace_id,scope_id);
CREATE TABLE triage_changes (
 workspace_id text NOT NULL,resource_id text NOT NULL,version text NOT NULL,
 after_id text NOT NULL DEFAULT '',at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,resource_id)
);
CREATE TABLE triage_model_attempts (
 workspace_id text NOT NULL,job_id text NOT NULL,number integer NOT NULL,
 resource_id text NOT NULL,input_id text NOT NULL,input_version text NOT NULL,
 lease text NOT NULL,actor text NOT NULL,response_version text,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,job_id,number),UNIQUE(workspace_id,resource_id)
);
CREATE INDEX triage_model_job ON triage_model_attempts(workspace_id,job_id);
CREATE INDEX triage_issue_history ON automation_records(workspace_id,(payload#>>'{scope,id}'),position DESC) WHERE kind='issue';

CREATE INDEX triage_model_period ON triage_model_attempts(workspace_id,created_at,job_id);
CREATE TABLE triage_sync (
 workspace_id text NOT NULL,scope_id text NOT NULL,
 due_at timestamptz NOT NULL DEFAULT now(),lease text NOT NULL DEFAULT '',lease_until timestamptz,checked_at timestamptz,observed_at timestamptz,
 PRIMARY KEY(workspace_id,scope_id)
);
CREATE INDEX triage_sync_due ON triage_sync(due_at,workspace_id,scope_id);

CREATE TABLE triage_bindings (
 workspace_id text NOT NULL,scope_id text NOT NULL,connection text NOT NULL,number bigint NOT NULL,source_id text NOT NULL,source_version text NOT NULL,due_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,scope_id)
);
CREATE INDEX triage_binding_due ON triage_bindings(due_at,workspace_id,scope_id);

CREATE TABLE triage_ranks(workspace_id text NOT NULL,scope_id text NOT NULL,policy_id text NOT NULL,policy_version text NOT NULL,rank integer NOT NULL,PRIMARY KEY(workspace_id,scope_id));
CREATE INDEX triage_rank_order ON triage_ranks(workspace_id,policy_id,policy_version,rank,scope_id);
CREATE INDEX triage_issue_period ON triage_targets(workspace_id,received_at,scope_id);
CREATE INDEX triage_scope_parent ON context_records(workspace_id,(payload#>>'{parent,id}'),resource_id,stream_sequence DESC) WHERE kind='scope';
CREATE TABLE triage_batch_jobs(workspace_id text NOT NULL,batch_id text NOT NULL,job_id text NOT NULL,PRIMARY KEY(workspace_id,batch_id,job_id));
CREATE INDEX triage_job_batch ON triage_batch_jobs(workspace_id,job_id,batch_id);
CREATE TABLE triage_run_links(workspace_id text NOT NULL,run_id text NOT NULL,scope_id text NOT NULL,received_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(workspace_id,run_id));
CREATE INDEX triage_run_issue ON triage_run_links(workspace_id,scope_id,received_at,run_id);

-- Existing Scope identities and manual/adopted values remain usable. Import
-- metadata only; migrations do not enqueue inference or enumerate Context bodies.
INSERT INTO triage_targets(workspace_id,scope_id,scope_version,received_at)
 SELECT c.workspace_id,c.resource_id,c.revision,COALESCE((SELECT min(j.created_at) FROM automation_jobs j WHERE j.workspace_id=c.workspace_id AND j.target_id=c.resource_id),now())
 FROM context_records c JOIN shared_versions h ON h.workspace_id=c.workspace_id AND h.resource_id=c.resource_id AND h.version=c.revision AND NOT h.deleted
 WHERE c.kind='scope' AND c.payload->>'kind'='issue';
INSERT INTO triage_ranks
 SELECT r.workspace_id,r.payload#>>'{target,id}',r.payload#>>'{policy,id}',r.payload#>>'{policy,version}',l.ordinality::int-1
 FROM automation_records r JOIN shared_versions h ON h.workspace_id=r.workspace_id AND h.resource_id=r.id AND h.version=r.version
 JOIN automation_records p ON p.workspace_id=r.workspace_id AND p.id=r.payload#>>'{policy,id}' AND p.version=r.payload#>>'{policy,version}'
 CROSS JOIN LATERAL jsonb_array_elements(p.payload->'levels') WITH ORDINALITY l(value,ordinality)
 WHERE r.kind='priority' AND r.payload->>'adopted'=l.value->>'id';
