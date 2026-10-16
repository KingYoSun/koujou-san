ALTER TABLE access_operations DROP CONSTRAINT access_operations_purpose_check;
ALTER TABLE access_operations ADD CHECK(purpose IN ('read','edit','price-edit','triage-edit','triage-manage'));
CREATE TABLE automation_records (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 version text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('policy','activation','control','priority','application')),
 position bigint NOT NULL,
 payload jsonb NOT NULL,
 refs jsonb NOT NULL,
 PRIMARY KEY(workspace_id,id,version)
);
CREATE INDEX automation_record_history ON automation_records(workspace_id,kind,id,position DESC);
CREATE TABLE automation_policy_targets (
 workspace_id text NOT NULL,
 scope_id text NOT NULL,
 policy_id text NOT NULL,
 PRIMARY KEY(workspace_id,scope_id,policy_id)
);
CREATE TABLE automation_jobs (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('hopper','triage')),
 event_id text NOT NULL,
 target_id text NOT NULL,
 target_version text NOT NULL,
 definition_id text NOT NULL,
 definition_version text NOT NULL,
 state text NOT NULL DEFAULT 'waiting',
 reason text NOT NULL DEFAULT '',
 worker text NOT NULL DEFAULT '',
 lease text NOT NULL DEFAULT '',
 lease_until timestamptz,
 attempts integer NOT NULL DEFAULT 0,
 due_at timestamptz NOT NULL DEFAULT now(),
 deadline timestamptz,
 checkpoint jsonb NOT NULL DEFAULT 'null',
 result jsonb NOT NULL DEFAULT 'null',
 refs jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id),
 UNIQUE(workspace_id,kind,event_id,target_id,target_version,definition_id,definition_version)
);
CREATE INDEX automation_job_due ON automation_jobs(kind,state,due_at,workspace_id,id);
CREATE INDEX automation_job_target ON automation_jobs(workspace_id,target_id,created_at DESC,id);
CREATE INDEX automation_job_slots ON automation_jobs(workspace_id,kind,lease_until) WHERE state='running';
CREATE TABLE automation_job_events (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 workspace_id text NOT NULL,
 job_id text NOT NULL,
 state text NOT NULL,
 reason text NOT NULL,
 worker text NOT NULL,
 lease text NOT NULL,
 attempt integer NOT NULL,
 checkpoint jsonb NOT NULL,
 at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(workspace_id,job_id) REFERENCES automation_jobs(workspace_id,id)
);
CREATE INDEX automation_job_event_page ON automation_job_events(workspace_id,job_id,sequence);

CREATE INDEX automation_issue_source ON context_records(workspace_id,resource_id,revision) WHERE kind='scope' AND payload->>'kind'='issue';
CREATE INDEX automation_event_budget ON automation_job_events(workspace_id,at) WHERE state='running';
