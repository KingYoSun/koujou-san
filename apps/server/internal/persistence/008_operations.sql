CREATE TABLE access_delivery_policies (
 workspace_id text PRIMARY KEY REFERENCES access_workspaces(id),
 payload jsonb NOT NULL
);
CREATE TABLE access_operations (
 workspace_id text NOT NULL REFERENCES access_workspaces(id),
 id text NOT NULL,
 user_id text NOT NULL REFERENCES auth_users(id),
 purpose text NOT NULL CHECK(purpose IN ('read','edit')),
 bounded boolean NOT NULL,
 closed boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id)
);
CREATE TABLE access_operation_refs (
 workspace_id text NOT NULL,
 operation_id text NOT NULL,
 resource_id text NOT NULL,
 version text NOT NULL,
 PRIMARY KEY(workspace_id,operation_id,resource_id,version),
 FOREIGN KEY(workspace_id,operation_id) REFERENCES access_operations(workspace_id,id)
);
CREATE TABLE access_operation_events (
 workspace_id text NOT NULL,
 operation_id text NOT NULL,
 id text NOT NULL,
 user_id text NOT NULL REFERENCES auth_users(id),
 method text NOT NULL,
 boundary text NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,operation_id,id),
 FOREIGN KEY(workspace_id,operation_id) REFERENCES access_operations(workspace_id,id)
);
CREATE INDEX operation_events_time ON access_operation_events(workspace_id,operation_id,created_at,id);
