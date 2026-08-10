DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/cloud-recordings/%';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/cloud-recordings/%');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/cloud-recordings' OR permission IN ('gb28181:recording:view','gb28181:recording:reconcile'));
DELETE FROM sys_menu WHERE path='/gb28181/cloud-recordings' OR permission IN ('gb28181:recording:view','gb28181:recording:reconcile');
DELETE FROM sys_api WHERE path LIKE '/api/gb28181/cloud-recordings/%';

DROP TABLE IF EXISTS gb_recording_reconcile_state;
UPDATE gb_recording_file SET start_time=COALESCE(start_time,TIMESTAMP '1970-01-01 00:00:00'),time_len=COALESCE(time_len,0),file_size=COALESCE(file_size,0);
ALTER TABLE gb_recording_file
  ALTER COLUMN start_time SET NOT NULL,
  ALTER COLUMN time_len SET DEFAULT 0,
  ALTER COLUMN time_len SET NOT NULL,
  ALTER COLUMN file_size SET DEFAULT 0,
  ALTER COLUMN file_size SET NOT NULL;
DROP INDEX IF EXISTS uk_recording_file_key;
DROP INDEX IF EXISTS idx_recording_file_date_tuple;
DROP INDEX IF EXISTS idx_recording_file_missing;
CREATE UNIQUE INDEX uk_recording_file_node_path ON gb_recording_file(node_id,file_path);
ALTER TABLE gb_recording_file
  DROP COLUMN updated_at, DROP COLUMN reconcile_miss_count, DROP COLUMN missing_at,
  DROP COLUMN last_seen_at, DROP COLUMN discovered_at, DROP COLUMN record_date,
  DROP COLUMN metadata_state, DROP COLUMN source, DROP COLUMN file_key,
  DROP COLUMN owner_dept_id, DROP COLUMN device_name, DROP COLUMN channel_name,
  DROP COLUMN channel_code;
