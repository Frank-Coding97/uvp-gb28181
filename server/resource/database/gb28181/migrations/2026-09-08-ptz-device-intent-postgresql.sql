-- Original PTZ authorization; historical rows deliberately remain NULL.
ALTER TABLE gb_ptz_operation ADD COLUMN IF NOT EXISTS device_epoch BIGINT NULL;
ALTER TABLE gb_ptz_operation ADD COLUMN IF NOT EXISTS device_intent_id VARCHAR(32) COLLATE "C" NULL;
ALTER TABLE gb_ptz_operation_attempt ADD COLUMN IF NOT EXISTS owner_process_id VARCHAR(32) COLLATE "C" NULL;
ALTER TABLE gb_ptz_operation_attempt ADD COLUMN IF NOT EXISTS owner_run_id VARCHAR(32) COLLATE "C" NULL;
ALTER TABLE gb_ptz_operation_attempt ADD COLUMN IF NOT EXISTS local_quiesced_at TIMESTAMP(6) NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uk_ptz_device_intent ON gb_ptz_operation(device_intent_id);
