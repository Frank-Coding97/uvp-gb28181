-- Split operator admission intent from ZLM heartbeat health (MySQL 5.7+).
SET @meta_node_exists := (
  SELECT COUNT(*) FROM information_schema.tables
  WHERE table_schema=DATABASE() AND table_name='meta_node'
);
SET @sql := IF(@meta_node_exists=1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='meta_node' AND column_name='enabled'
), 'ALTER TABLE `meta_node` ADD COLUMN `enabled` tinyint(1) NOT NULL DEFAULT 1 AFTER `state`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `meta_node` SET `enabled`=0,`state`='offline' WHERE `state`='maintenance';

SET @sql := IF(@meta_node_exists=1 AND NOT EXISTS (
  SELECT 1 FROM information_schema.statistics
  WHERE table_schema=DATABASE() AND table_name='meta_node' AND index_name='idx_enabled'
), 'CREATE INDEX `idx_enabled` ON `meta_node` (`enabled`)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '启用媒体节点','/api/gb28181/zlm/nodes/:id/enable','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/enable' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '停用媒体节点','/api/gb28181/zlm/nodes/:id/disable','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/disable' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable')
  AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable')
  AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND CONVERT(p.v1 USING utf8mb4) COLLATE utf8mb4_unicode_ci=a.path AND CONVERT(p.v2 USING utf8mb4) COLLATE utf8mb4_unicode_ci=a.method AND p.v3='*');
