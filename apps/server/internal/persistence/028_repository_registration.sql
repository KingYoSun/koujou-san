CREATE INDEX shared_active_repository_catalog
 ON shared_versions(workspace_id,resource_id)
 WHERE NOT deleted AND resource_id LIKE 'forge-repository:%';
