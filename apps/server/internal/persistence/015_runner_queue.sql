CREATE INDEX access_device_run_queue ON access_resources(workspace_id,assigned_device,id)
 WHERE assigned_device IS NOT NULL AND id LIKE 'run:%';
