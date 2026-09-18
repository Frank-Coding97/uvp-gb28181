DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1 IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND v2='POST';
DELETE ma FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id
WHERE a.path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND a.method='POST';
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND method='POST' AND deleted_at IS NULL;

SET @meta_node_enabled_exists := (
  SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='meta_node' AND column_name='enabled'
);
SET @sql := IF(@meta_node_enabled_exists=1, 'UPDATE `meta_node` SET `state`=''maintenance'' WHERE `enabled`=0', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @sql := IF(EXISTS (
  SELECT 1 FROM information_schema.statistics
  WHERE table_schema=DATABASE() AND table_name='meta_node' AND index_name='idx_enabled'
), 'DROP INDEX `idx_enabled` ON `meta_node`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
SET @sql := IF(@meta_node_enabled_exists=1, 'ALTER TABLE `meta_node` DROP COLUMN `enabled`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
