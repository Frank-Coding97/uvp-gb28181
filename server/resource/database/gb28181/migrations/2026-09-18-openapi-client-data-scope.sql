-- OpenAPI 客户端数据范围：3=本部门，4=本部门及以下。
-- 旧客户端统一按 3（本部门）处理，避免升级后扩大既有凭证的可见范围。
-- MySQL 8 没有可移植的 ADD COLUMN IF NOT EXISTS，以下守卫保证重复执行幂等。

SET @openapi_schema_name := DATABASE();
SET @openapi_data_scope_exists := (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @openapi_schema_name
    AND TABLE_NAME = 'sys_openapi_client'
    AND COLUMN_NAME = 'data_scope'
);
SET @openapi_data_scope_sql := IF(
  @openapi_data_scope_exists = 0,
  'ALTER TABLE `sys_openapi_client` ADD COLUMN `data_scope` TINYINT NOT NULL DEFAULT 3 COMMENT ''数据范围 3本部门 4本部门及以下'' AFTER `owner_dept_id`',
  'SELECT 1'
);
PREPARE openapi_data_scope_stmt FROM @openapi_data_scope_sql;
EXECUTE openapi_data_scope_stmt;
DEALLOCATE PREPARE openapi_data_scope_stmt;

-- 兼容早期试运行版本或人工写入的非法值，先收敛到最小权限再加约束。
UPDATE `sys_openapi_client`
SET `data_scope` = 3
WHERE `data_scope` IS NULL OR `data_scope` NOT IN (3, 4);

SET @openapi_data_scope_constraint_exists := (
  SELECT COUNT(*)
  FROM information_schema.TABLE_CONSTRAINTS
  WHERE CONSTRAINT_SCHEMA = @openapi_schema_name
    AND TABLE_NAME = 'sys_openapi_client'
    AND CONSTRAINT_NAME = 'ck_openapi_client_data_scope'
);
SET @openapi_data_scope_constraint_sql := IF(
  @openapi_data_scope_constraint_exists = 0,
  'ALTER TABLE `sys_openapi_client` ADD CONSTRAINT `ck_openapi_client_data_scope` CHECK (`data_scope` IN (3,4))',
  'SELECT 1'
);
PREPARE openapi_data_scope_constraint_stmt FROM @openapi_data_scope_constraint_sql;
EXECUTE openapi_data_scope_constraint_stmt;
DEALLOCATE PREPARE openapi_data_scope_constraint_stmt;
