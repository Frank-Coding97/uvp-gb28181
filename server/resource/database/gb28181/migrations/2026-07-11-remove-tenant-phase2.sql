-- 2026-07-11 Phase 2: 物理移除多租户 schema 残留
-- 说明:
-- 1. Phase 1 必须先完成 tenant_id -> owner_dept_id 回填。
-- 2. 若任一 GB28181 行仍然 owner_dept_id=0,则直接阻断本阶段,避免删除唯一可恢复映射。
-- 3. MySQL 5.7 普通脚本里不能直接 SIGNAL;这里故意查询不存在的 __owner_dept_* 表来中断迁移。

SET @gb_owner_column_count := (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name IN ('gb_device', 'gb_channel', 'gb_catalog_node', 'gb_channel_mount', 'gb_anomaly_record')
    AND column_name = 'owner_dept_id'
);

SET @sql := IF(
  @gb_owner_column_count = 5,
  'SET @gb_owner_zero_count := (
     (SELECT COUNT(*) FROM `gb_device` WHERE `owner_dept_id` = 0) +
     (SELECT COUNT(*) FROM `gb_channel` WHERE `owner_dept_id` = 0) +
     (SELECT COUNT(*) FROM `gb_catalog_node` WHERE `owner_dept_id` = 0) +
     (SELECT COUNT(*) FROM `gb_channel_mount` WHERE `owner_dept_id` = 0) +
     (SELECT COUNT(*) FROM `gb_anomaly_record` WHERE `owner_dept_id` = 0)
   )',
  'SELECT * FROM `__owner_dept_phase1_required__`'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @gb_owner_zero_count = 0,
  'SELECT 1',
  'SELECT * FROM `__owner_dept_backfill_required__`'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= GB28181 tenant 索引 =========
SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_device` DROP INDEX `idx_tenant_deleted`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_device' AND index_name = 'idx_tenant_deleted');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_channel` DROP INDEX `idx_tenant_deleted`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_channel' AND index_name = 'idx_tenant_deleted');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP INDEX `idx_tenant_parent`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND index_name = 'idx_tenant_parent');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP INDEX `idx_tenant_path`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND index_name = 'idx_tenant_path');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP INDEX `idx_tenant_type`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND index_name = 'idx_tenant_type');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP INDEX `idx_tenant_anomaly`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND index_name = 'idx_tenant_anomaly');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP INDEX `idx_civil_code`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND index_name = 'idx_civil_code');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_channel_mount` DROP INDEX `idx_tenant`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_channel_mount' AND index_name = 'idx_tenant');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_anomaly_record` DROP INDEX `idx_tenant_resolved`', 'SELECT 1')
  FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'gb_anomaly_record' AND index_name = 'idx_tenant_resolved');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= GB28181 tenant 列 =========
SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_device` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'gb_device' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_channel` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'gb_channel' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_catalog_node` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'gb_catalog_node' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_channel_mount` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'gb_channel_mount' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `gb_anomaly_record` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'gb_anomaly_record' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= 通用 tenant 列 =========
SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `demo_students` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'demo_students' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `example` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'example' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_affix` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_affix' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_affix_chunk` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_affix_chunk' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_department` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_department' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_api` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_api' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_jobs` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_jobs' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_operation_logs` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_operation_logs' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_role` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_role' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := (SELECT IF(COUNT(*) > 0, 'ALTER TABLE `sys_users` DROP COLUMN `tenant_id`', 'SELECT 1')
  FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'sys_users' AND column_name = 'tenant_id');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- ========= 代码生成元数据与历史租户表 =========
DELETE FROM `sys_gen_field` WHERE `data_name` = 'tenant_id';
DROP TABLE IF EXISTS `sys_user_tenant`;
DROP TABLE IF EXISTS `sys_tenants`;
