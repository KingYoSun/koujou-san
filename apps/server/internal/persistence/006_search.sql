CREATE INDEX context_membership_targets ON context_records
 (workspace_id,(payload->'context'->>'id'),resource_id)
 WHERE kind='membership';
