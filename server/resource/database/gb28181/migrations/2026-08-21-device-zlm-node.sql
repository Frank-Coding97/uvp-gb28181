-- 设备首选 ZLM 节点；0 表示继续使用集群自动调度。
SET @device_zlm_column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND COLUMN_NAME = 'zlm_node_id'
);
SET @device_zlm_column_sql := IF(
  @device_zlm_column_exists = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `zlm_node_id` bigint NOT NULL DEFAULT 0 COMMENT ''首选 ZLM 节点,0 表示自动调度'' AFTER `effective_version_at`',
  'SELECT 1'
);
PREPARE device_zlm_column_stmt FROM @device_zlm_column_sql;
EXECUTE device_zlm_column_stmt;
DEALLOCATE PREPARE device_zlm_column_stmt;

SET @device_zlm_index_exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_device' AND INDEX_NAME = 'idx_gb_device_zlm_node'
);
SET @device_zlm_index_sql := IF(
  @device_zlm_index_exists = 0,
  'ALTER TABLE `gb_device` ADD INDEX `idx_gb_device_zlm_node` (`zlm_node_id`)',
  'SELECT 1'
);
PREPARE device_zlm_index_stmt FROM @device_zlm_index_sql;
EXECUTE device_zlm_index_stmt;
DEALLOCATE PREPARE device_zlm_index_stmt;
