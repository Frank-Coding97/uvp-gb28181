-- Durable ZLM node revision and fail-closed endpoint recovery state (MySQL 5.7+).
-- Every statement is safe to rerun and is a no-op when meta_node is absent.
SET @meta_node_exists := (
  SELECT COUNT(*) FROM information_schema.tables
  WHERE table_schema = DATABASE() AND table_name = 'meta_node'
);

SET @sql := IF(@meta_node_exists = 1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'meta_node' AND column_name = 'revision'
), 'ALTER TABLE `meta_node` ADD COLUMN `revision` bigint unsigned NOT NULL DEFAULT 1 AFTER `id`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @sql := IF(@meta_node_exists = 1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'meta_node' AND column_name = 'recovery_required'
), 'ALTER TABLE `meta_node` ADD COLUMN `recovery_required` tinyint(1) NOT NULL DEFAULT 0 AFTER `state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(@meta_node_exists = 1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'meta_node' AND column_name = 'recovery_reason'
), 'ALTER TABLE `meta_node` ADD COLUMN `recovery_reason` varchar(255) NOT NULL DEFAULT '''' AFTER `recovery_required`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(@meta_node_exists = 1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'meta_node' AND column_name = 'recovery_fingerprint'
), 'ALTER TABLE `meta_node` ADD COLUMN `recovery_fingerprint` char(64) NOT NULL DEFAULT '''' AFTER `recovery_reason`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
