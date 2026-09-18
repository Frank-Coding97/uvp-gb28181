-- RTP-only fixed evidence. NULL preserves unknown legacy history.
-- TEXT preserves canonical bytes; the application validates the full contract.
SET @rtp_steps_column_exists := (SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'gb_device_operation_intent' AND column_name = 'rtp_steps_json');
SET @rtp_steps_sql := IF(@rtp_steps_column_exists = 0,
    'ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL', 'SELECT 1');
PREPARE rtp_steps_stmt FROM @rtp_steps_sql;
EXECUTE rtp_steps_stmt;
DEALLOCATE PREPARE rtp_steps_stmt;
SET @rtp_steps_constraint_exists := (SELECT COUNT(*) FROM information_schema.table_constraints
    WHERE constraint_schema = DATABASE() AND table_name = 'gb_device_operation_intent' AND constraint_name = 'ck_device_intent_rtp_size');
SET @rtp_steps_sql := IF(@rtp_steps_constraint_exists = 0,
    'ALTER TABLE gb_device_operation_intent ADD CONSTRAINT ck_device_intent_rtp_size CHECK (rtp_steps_json IS NULL OR OCTET_LENGTH(rtp_steps_json) BETWEEN 1 AND 32768)', 'SELECT 1');
PREPARE rtp_steps_stmt FROM @rtp_steps_sql;
EXECUTE rtp_steps_stmt;
DEALLOCATE PREPARE rtp_steps_stmt;
