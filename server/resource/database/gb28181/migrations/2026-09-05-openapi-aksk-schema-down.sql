-- Isolated empty test databases only. Never run on live authorization state or
-- use this file to delete business rows. The Down executor must verify the
-- media/device baseline before executing these feature-only inverse operations.
DROP TABLE IF EXISTS gb_openapi_viewer;
DROP TABLE IF EXISTS gb_openapi_play_grant;

SET @openapi_schema_name := DATABASE();
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_identity_status')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `runtime_identity_status`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_confirmed_at')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `runtime_confirmed_at`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_confirmed_revision')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `runtime_confirmed_revision`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_protocol_version')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `runtime_protocol_version`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_epoch')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `runtime_epoch`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='retired_boot_history')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `retired_boot_history`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='current_boot_nonce')=1,
    'ALTER TABLE `meta_node` DROP COLUMN `current_boot_nonce`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='gb_device' AND column_name='legacy_revoked_before')=1,
    'ALTER TABLE `gb_device` DROP COLUMN `legacy_revoked_before`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='gb_device' AND column_name='access_epoch')=1,
    'ALTER TABLE `gb_device` DROP COLUMN `access_epoch`', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql; EXECUTE openapi_stmt; DEALLOCATE PREPARE openapi_stmt;

DROP TABLE IF EXISTS sys_openapi_audit;
DROP TABLE IF EXISTS sys_openapi_nonce;
DROP TABLE IF EXISTS sys_openapi_client_scope;
DROP TABLE IF EXISTS sys_openapi_client;
