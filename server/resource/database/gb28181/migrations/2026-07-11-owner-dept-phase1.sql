-- 2026-07-11 Phase 1: 将 GB28181 运行时权限迁移到 owner_dept_id
-- 说明:
-- 1. 保留历史 tenant_id, 先补 owner_dept_id 字段/索引并完成历史回填。
-- 2. 回填策略: 优先使用 sys_user_tenant -> sys_users.dept_id; 多用户命中同一 tenant 时取最小非 0 dept_id。
-- 3. 若历史 tenant 无法映射到任何 dept_id,则保持 owner_dept_id=0,由 Phase 2 阻断继续删除 tenant 资产。
-- 4. MySQL 5.7+ 可重复执行。

SET @schema_name := DATABASE();

-- ========= owner_dept_id 字段 =========
SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'owner_dept_id') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_channel' AND column_name = 'owner_dept_id') = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND column_name = 'owner_dept_id') = 0,
  'ALTER TABLE `gb_catalog_node` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_channel_mount' AND column_name = 'owner_dept_id') = 0,
  'ALTER TABLE `gb_channel_mount` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_anomaly_record' AND column_name = 'owner_dept_id') = 0,
  'ALTER TABLE `gb_anomaly_record` ADD COLUMN `owner_dept_id` int unsigned NOT NULL DEFAULT 0 COMMENT ''所属部门ID''',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= owner_dept_id 索引 =========
SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_device' AND index_name = 'idx_owner_dept_deleted') = 0,
  'ALTER TABLE `gb_device` ADD INDEX `idx_owner_dept_deleted` (`owner_dept_id`, `deleted_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_channel' AND index_name = 'idx_owner_dept_deleted') = 0,
  'ALTER TABLE `gb_channel` ADD INDEX `idx_owner_dept_deleted` (`owner_dept_id`, `deleted_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_owner_dept_parent') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_parent` (`owner_dept_id`, `parent_id`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_owner_dept_path') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_path` (`owner_dept_id`, `path`(128))',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_owner_dept_type') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_type` (`owner_dept_id`, `node_type`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_owner_dept_anomaly') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_anomaly` (`owner_dept_id`, `anomaly`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_owner_dept_civil_code') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_owner_dept_civil_code` (`owner_dept_id`, `civil_code`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_channel_mount' AND index_name = 'idx_owner_dept') = 0,
  'ALTER TABLE `gb_channel_mount` ADD INDEX `idx_owner_dept` (`owner_dept_id`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_anomaly_record' AND index_name = 'idx_owner_dept_resolved') = 0,
  'ALTER TABLE `gb_anomaly_record` ADD INDEX `idx_owner_dept_resolved` (`owner_dept_id`, `resolved`, `created_at`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= tenant -> owner_dept 历史回填 =========
DROP TEMPORARY TABLE IF EXISTS `tmp_tenant_owner_dept`;
CREATE TEMPORARY TABLE `tmp_tenant_owner_dept` (
  `tenant_id` bigint unsigned NOT NULL PRIMARY KEY,
  `owner_dept_id` int unsigned NOT NULL
) ENGINE=Memory;

SET @tenant_map_available := (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = @schema_name
    AND (
      (table_name = 'sys_user_tenant' AND column_name IN ('tenant_id', 'user_id'))
      OR (table_name = 'sys_users' AND column_name IN ('id', 'dept_id'))
    )
) = 4;

SET @sql := IF(
  @tenant_map_available,
  'INSERT INTO `tmp_tenant_owner_dept` (`tenant_id`, `owner_dept_id`)
   SELECT ut.`tenant_id`, MIN(u.`dept_id`)
   FROM `sys_user_tenant` ut
   JOIN `sys_users` u ON u.`id` = ut.`user_id`
   WHERE ut.`tenant_id` <> 0
     AND u.`dept_id` IS NOT NULL
     AND u.`dept_id` <> 0
   GROUP BY ut.`tenant_id`',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @tenant_map_available
    AND (SELECT COUNT(*) FROM information_schema.columns
         WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'tenant_id') = 1,
  'UPDATE `gb_device` d
   JOIN `tmp_tenant_owner_dept` m ON m.`tenant_id` = d.`tenant_id`
   SET d.`owner_dept_id` = m.`owner_dept_id`
   WHERE d.`owner_dept_id` = 0 AND d.`tenant_id` <> 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @tenant_map_available
    AND (SELECT COUNT(*) FROM information_schema.columns
         WHERE table_schema = @schema_name AND table_name = 'gb_channel' AND column_name = 'tenant_id') = 1,
  'UPDATE `gb_channel` ch
   JOIN `tmp_tenant_owner_dept` m ON m.`tenant_id` = ch.`tenant_id`
   SET ch.`owner_dept_id` = m.`owner_dept_id`
   WHERE ch.`owner_dept_id` = 0 AND ch.`tenant_id` <> 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @tenant_map_available
    AND (SELECT COUNT(*) FROM information_schema.columns
         WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND column_name = 'tenant_id') = 1,
  'UPDATE `gb_catalog_node` n
   JOIN `tmp_tenant_owner_dept` m ON m.`tenant_id` = n.`tenant_id`
   SET n.`owner_dept_id` = m.`owner_dept_id`
   WHERE n.`owner_dept_id` = 0 AND n.`tenant_id` <> 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @tenant_map_available
    AND (SELECT COUNT(*) FROM information_schema.columns
         WHERE table_schema = @schema_name AND table_name = 'gb_channel_mount' AND column_name = 'tenant_id') = 1,
  'UPDATE `gb_channel_mount` mnt
   JOIN `tmp_tenant_owner_dept` m ON m.`tenant_id` = mnt.`tenant_id`
   SET mnt.`owner_dept_id` = m.`owner_dept_id`
   WHERE mnt.`owner_dept_id` = 0 AND mnt.`tenant_id` <> 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @tenant_map_available
    AND (SELECT COUNT(*) FROM information_schema.columns
         WHERE table_schema = @schema_name AND table_name = 'gb_anomaly_record' AND column_name = 'tenant_id') = 1,
  'UPDATE `gb_anomaly_record` ar
   JOIN `tmp_tenant_owner_dept` m ON m.`tenant_id` = ar.`tenant_id`
   SET ar.`owner_dept_id` = m.`owner_dept_id`
   WHERE ar.`owner_dept_id` = 0 AND ar.`tenant_id` <> 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

DROP TEMPORARY TABLE IF EXISTS `tmp_tenant_owner_dept`;

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
