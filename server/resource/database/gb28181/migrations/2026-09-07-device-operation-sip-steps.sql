-- SIP INVITE-only fixed evidence. NULL preserves unknown legacy history.
-- TEXT preserves canonical bytes; the application validates the full contract.
SET @sip_steps_column_exists := (SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'gb_device_operation_intent' AND column_name = 'sip_steps_json');
SET @sip_steps_sql := IF(@sip_steps_column_exists = 0,
    'ALTER TABLE gb_device_operation_intent ADD COLUMN sip_steps_json TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NULL', 'SELECT 1');
PREPARE sip_steps_stmt FROM @sip_steps_sql;
EXECUTE sip_steps_stmt;
DEALLOCATE PREPARE sip_steps_stmt;
SET @sip_steps_constraint_exists := (SELECT COUNT(*) FROM information_schema.table_constraints
    WHERE constraint_schema = DATABASE() AND table_name = 'gb_device_operation_intent' AND constraint_name = 'ck_device_intent_sip_size');
SET @sip_steps_sql := IF(@sip_steps_constraint_exists = 0,
    'ALTER TABLE gb_device_operation_intent ADD CONSTRAINT ck_device_intent_sip_size CHECK (sip_steps_json IS NULL OR OCTET_LENGTH(sip_steps_json) BETWEEN 1 AND 32768)', 'SELECT 1');
PREPARE sip_steps_stmt FROM @sip_steps_sql;
EXECUTE sip_steps_stmt;
DEALLOCATE PREPARE sip_steps_stmt;
