-- Catalog 节点按上报来源隔离。
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_catalog_node'
);
SET @column_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_catalog_node' AND COLUMN_NAME = 'source_device_id'
);
SET @ddl := IF(@table_exists = 1 AND @column_exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD COLUMN `source_device_id` varchar(20) NOT NULL DEFAULT '''' COMMENT ''目录项上报来源设备或下级平台编码'' AFTER `civil_code`',
  'SELECT 1');
PREPARE catalog_node_source_device_stmt FROM @ddl;
EXECUTE catalog_node_source_device_stmt;
DEALLOCATE PREPARE catalog_node_source_device_stmt;
SET @index_exists := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_catalog_node' AND INDEX_NAME = 'idx_owner_source_type_code'
);
SET @ddl := IF(@table_exists = 1 AND @index_exists = 0,
  'CREATE INDEX `idx_owner_source_type_code` ON `gb_catalog_node` (`owner_dept_id`, `source_device_id`, `node_type`, `code`)',
  'SELECT 1');
PREPARE catalog_node_source_device_index_stmt FROM @ddl;
EXECUTE catalog_node_source_device_index_stmt;
DEALLOCATE PREPARE catalog_node_source_device_index_stmt;
-- 可由物理关联确定来源的历史节点安全回填；无法唯一归因的业务节点保留空值。
UPDATE `gb_catalog_node` n
JOIN `gb_channel` c ON c.`id` = n.`channel_id`
SET n.`source_device_id` = c.`device_id`
WHERE n.`source_device_id` = '' AND c.`device_id` <> ''
  AND n.`node_type` = 'channel' AND n.`source` = 'catalog'
  AND n.`owner_dept_id` = c.`owner_dept_id`
  AND n.`deleted_at` IS NULL AND c.`deleted_at` IS NULL;
