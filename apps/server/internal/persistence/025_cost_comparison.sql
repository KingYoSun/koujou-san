ALTER TABLE usage_records DROP CONSTRAINT usage_records_kind_check;
ALTER TABLE usage_records ADD CHECK(kind IN ('usage','price','evaluation','comparison'));
CREATE INDEX usage_entry_basis ON usage_entries USING gin(refs jsonb_path_ops);
CREATE INDEX usage_entry_coverage ON usage_entries(workspace_id,coverage_group,kind,projection_key,position DESC);
