CREATE TABLE access_workspaces (id text PRIMARY KEY, name text NOT NULL, created_by text NOT NULL REFERENCES auth_users(id));
CREATE TABLE access_members (
    workspace_id text NOT NULL REFERENCES access_workspaces(id), user_id text NOT NULL REFERENCES auth_users(id),
    role text NOT NULL CHECK(role IN ('admin','editor','viewer')), can_execute boolean NOT NULL DEFAULT false, active boolean NOT NULL DEFAULT true,
    PRIMARY KEY(workspace_id,user_id)
);
CREATE TABLE access_invites (
    id text PRIMARY KEY, workspace_id text NOT NULL REFERENCES access_workspaces(id), role text NOT NULL CHECK(role IN ('admin','editor','viewer')),
    expires_at timestamptz NOT NULL, cancelled boolean NOT NULL DEFAULT false, accepted_by text REFERENCES auth_users(id)
);
CREATE INDEX access_invites_workspace ON access_invites(workspace_id,id);
CREATE TABLE access_devices (
    id text PRIMARY KEY, workspace_id text NOT NULL REFERENCES access_workspaces(id), registered_by text NOT NULL REFERENCES auth_users(id),
    identifier text NOT NULL, name text NOT NULL, generation text NOT NULL, enabled boolean NOT NULL DEFAULT true,
    can_input boolean NOT NULL DEFAULT false, can_report boolean NOT NULL DEFAULT false, last_seen timestamptz,
    UNIQUE(workspace_id,registered_by,identifier)
);
CREATE INDEX access_devices_workspace ON access_devices(workspace_id,id);
CREATE INDEX access_member_user ON access_members(user_id,workspace_id);
-- Domain services supply these authoritative boundaries, never a client-side Scope/containment claim.
CREATE TABLE access_resources (
    workspace_id text NOT NULL REFERENCES access_workspaces(id), id text NOT NULL, restricted boolean NOT NULL,
    assigned_device text REFERENCES access_devices(id), PRIMARY KEY(workspace_id,id)
);
CREATE TABLE access_resource_grants (
    workspace_id text NOT NULL, resource_id text NOT NULL, user_id text NOT NULL REFERENCES auth_users(id),
    can_read boolean NOT NULL DEFAULT false, can_update boolean NOT NULL DEFAULT false, can_execute boolean NOT NULL DEFAULT false,
    PRIMARY KEY(workspace_id,resource_id,user_id), FOREIGN KEY(workspace_id,resource_id) REFERENCES access_resources(workspace_id,id)
);
