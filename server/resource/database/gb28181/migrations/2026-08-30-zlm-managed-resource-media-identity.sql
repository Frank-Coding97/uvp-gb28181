-- Add the complete non-secret ZLM media identity to the management ledger.
-- Existing rows intentionally remain empty/unknown; no identity is inferred.
-- MySQL 5.7+.
SET @schema_name := DATABASE();

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_zlm_managed_resource') = 1
  AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_zlm_managed_resource' AND column_name = 'schema') = 0,
  'ALTER TABLE `gb_zlm_managed_resource` ADD COLUMN `schema` varchar(32) NOT NULL DEFAULT '''' AFTER `resource_key`',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_zlm_managed_resource') = 1
  AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_zlm_managed_resource' AND column_name = 'vhost') = 0,
  'ALTER TABLE `gb_zlm_managed_resource` ADD COLUMN `vhost` varchar(128) NOT NULL DEFAULT '''' AFTER `schema`',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- The original key omitted media identity. Replace it with the exact tuple.
SET @managed_resource_unique_columns := (
  SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',')
  FROM information_schema.statistics
  WHERE table_schema = @schema_name
    AND table_name = 'gb_zlm_managed_resource'
    AND index_name = 'uk_gb_zlm_managed_resource_identity'
);
SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_zlm_managed_resource') = 0,
  'SELECT 1',
  IF(@managed_resource_unique_columns IS NULL,
    'ALTER TABLE `gb_zlm_managed_resource` ADD UNIQUE INDEX `uk_gb_zlm_managed_resource_identity` (`node_id`,`resource_type`,`resource_key`,`schema`,`vhost`,`app`,`stream`)',
    IF(@managed_resource_unique_columns <> 'node_id,resource_type,resource_key,schema,vhost,app,stream',
      'ALTER TABLE `gb_zlm_managed_resource` DROP INDEX `uk_gb_zlm_managed_resource_identity`, ADD UNIQUE INDEX `uk_gb_zlm_managed_resource_identity` (`node_id`,`resource_type`,`resource_key`,`schema`,`vhost`,`app`,`stream`)',
      'SELECT 1'
    )
  )
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
