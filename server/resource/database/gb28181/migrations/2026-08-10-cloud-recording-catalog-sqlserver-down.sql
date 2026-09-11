DELETE FROM [sys_casbin_rule] WHERE [v1] LIKE '/api/gb28181/cloud-recordings/%';
DELETE FROM [sys_menu_api] WHERE [api_id] IN (SELECT [id] FROM [sys_api] WHERE [path] LIKE '/api/gb28181/cloud-recordings/%');
DELETE FROM [sys_role_menu] WHERE [menu_id] IN (SELECT [id] FROM [sys_menu] WHERE [path]='/gb28181/cloud-recordings' OR [permission] IN ('gb28181:recording:view','gb28181:recording:reconcile'));
DELETE FROM [sys_menu] WHERE [path]='/gb28181/cloud-recordings' OR [permission] IN ('gb28181:recording:view','gb28181:recording:reconcile');
DELETE FROM [sys_api] WHERE [path] LIKE '/api/gb28181/cloud-recordings/%';

DROP TABLE IF EXISTS gb_recording_reconcile_state;
UPDATE gb_recording_file SET start_time=ISNULL(start_time,CONVERT(datetime2,'1970-01-01T00:00:00')),time_len=ISNULL(time_len,0),file_size=ISNULL(file_size,0);
ALTER TABLE gb_recording_file ALTER COLUMN start_time datetime2 NOT NULL;
ALTER TABLE gb_recording_file ALTER COLUMN time_len decimal(12,3) NOT NULL;
ALTER TABLE gb_recording_file ALTER COLUMN file_size bigint NOT NULL;
DROP INDEX IF EXISTS uk_recording_file_key ON gb_recording_file;
DROP INDEX IF EXISTS idx_recording_file_date_tuple ON gb_recording_file;
DROP INDEX IF EXISTS idx_recording_file_missing ON gb_recording_file;
CREATE UNIQUE INDEX uk_recording_file_node_path ON gb_recording_file(node_id,file_path);
ALTER TABLE gb_recording_file DROP CONSTRAINT DF_recording_file_channel_code, DF_recording_file_channel_name, DF_recording_file_device_name, DF_recording_file_owner_dept, DF_recording_file_source, DF_recording_file_metadata, DF_recording_file_miss;
ALTER TABLE gb_recording_file DROP COLUMN updated_at, reconcile_miss_count, missing_at, last_seen_at, discovered_at, record_date, metadata_state, source, file_key, owner_dept_id, device_name, channel_name, channel_code;
