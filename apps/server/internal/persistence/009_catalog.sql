CREATE INDEX shared_scope_catalog ON shared_versions(workspace_id,resource_id) WHERE resource_id LIKE 'scope:%';
