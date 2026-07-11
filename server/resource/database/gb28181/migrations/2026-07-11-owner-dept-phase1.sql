-- 2026-07-11 Phase 1: 将 GB28181 运行时权限迁移到 owner_dept_id
-- 说明: 保留历史 tenant_id, 只补部门归属字段/索引并下线租户菜单与 API seed。
-- MySQL 5.7+ 可重复执行。

SET @schema_name := DATABASE();

-- ========= gb_device =========
SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_device'
  AND COLUMN_NAME = 'owner_dept_id';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_device'
  AND INDEX_NAME = 'idx_owner_dept_deleted';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_device` ADD INDEX `idx_owner_dept_deleted` (`owner_dept_id`, `deleted_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========= gb_channel =========
SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_channel'
  AND COLUMN_NAME = 'owner_dept_id';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_channel'
  AND INDEX_NAME = 'idx_owner_dept_deleted';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_channel` ADD INDEX `idx_owner_dept_deleted` (`owner_dept_id`, `deleted_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========= gb_catalog_node =========
SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND COLUMN_NAME = 'owner_dept_id';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND INDEX_NAME = 'idx_owner_dept_parent';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_parent` (`owner_dept_id`, `parent_id`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND INDEX_NAME = 'idx_owner_dept_path';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_path` (`owner_dept_id`, `path`(128))',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND INDEX_NAME = 'idx_owner_dept_type';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_type` (`owner_dept_id`, `node_type`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND INDEX_NAME = 'idx_owner_dept_anomaly';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_anomaly` (`owner_dept_id`, `anomaly`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_catalog_node'
  AND INDEX_NAME = 'idx_owner_dept_civil_code';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_civil_code` (`owner_dept_id`, `civil_code`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========= gb_channel_mount =========
SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_channel_mount'
  AND COLUMN_NAME = 'owner_dept_id';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_channel_mount` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_channel_mount'
  AND INDEX_NAME = 'idx_owner_dept';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_channel_mount` ADD INDEX `idx_owner_dept` (`owner_dept_id`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========= gb_anomaly_record =========
SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_anomaly_record'
  AND COLUMN_NAME = 'owner_dept_id';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_anomaly_record` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SELECT COUNT(*) INTO @exists
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = @schema_name
  AND TABLE_NAME = 'gb_anomaly_record'
  AND INDEX_NAME = 'idx_owner_dept_resolved';
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE `gb_anomaly_record` ADD INDEX `idx_owner_dept_resolved` (`owner_dept_id`, `resolved`, `created_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========= 下线租户菜单/API 权限 seed =========
DELETE FROM `sys_menu_api`
WHERE `menu_id` IN (
  SELECT `id`
  FROM `sys_menu`
  WHERE `path` = '/system/systenant'
     OR `permission` IN (
       'system:tenant:add',
       'system:tenant:edit',
       'system:tenant:delete',
       'system:tenant:assignUser'
     )
);

DELETE FROM `sys_role_menu`
WHERE `menu_id` IN (
  SELECT `id`
  FROM `sys_menu`
  WHERE `path` = '/system/systenant'
     OR `permission` IN (
       'system:tenant:add',
       'system:tenant:edit',
       'system:tenant:delete',
       'system:tenant:assignUser'
     )
);

DELETE FROM `sys_casbin_rule`
WHERE `v1` LIKE '/api/sysTenant/%'
   OR `v1` LIKE '/api/sysUserTenant/%';

DELETE FROM `sys_menu`
WHERE `path` = '/system/systenant'
   OR `permission` IN (
     'system:tenant:add',
     'system:tenant:edit',
     'system:tenant:delete',
     'system:tenant:assignUser'
   );

DELETE FROM `sys_api`
WHERE `path` IN (
  '/api/sysTenant/list',
  '/api/sysTenant/:id',
  '/api/sysTenant/add',
  '/api/sysTenant/edit',
  '/api/sysUserTenant/list',
  '/api/sysUserTenant/get',
  '/api/sysUserTenant/batchAdd',
  '/api/sysUserTenant/batchDelete',
  '/api/sysUserTenant/userListAll',
  '/api/sysUserTenant/getRolesAll',
  '/api/sysUserTenant/setUserRoles',
  '/api/sysUserTenant/getUserRoleIDs'
);
