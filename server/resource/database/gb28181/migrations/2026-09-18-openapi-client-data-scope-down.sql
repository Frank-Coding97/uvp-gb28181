-- 回滚 OpenAPI 客户端数据范围字段。
-- 仅用于已确认的隔离环境；删除该列会丢弃每个客户端的范围选择。

SET @openapi_schema_name := DATABASE();
SET @openapi_data_scope_constraint_exists := (
  SELECT COUNT(*)
  FROM information_schema.TABLE_CONSTRAINTS
  WHERE CONSTRAINT_SCHEMA = @openapi_schema_name
    AND TABLE_NAME = 'sys_openapi_client'
    AND CONSTRAINT_NAME = 'ck_openapi_client_data_scope'
);
SET @openapi_data_scope_constraint_sql := IF(
  @openapi_data_scope_constraint_exists = 1,
  'ALTER TABLE `sys_openapi_client` DROP CHECK `ck_openapi_client_data_scope`',
  'SELECT 1'
);
PREPARE openapi_data_scope_constraint_stmt FROM @openapi_data_scope_constraint_sql;
EXECUTE openapi_data_scope_constraint_stmt;
DEALLOCATE PREPARE openapi_data_scope_constraint_stmt;

SET @openapi_data_scope_exists := (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @openapi_schema_name
    AND TABLE_NAME = 'sys_openapi_client'
    AND COLUMN_NAME = 'data_scope'
);
SET @openapi_data_scope_sql := IF(
  @openapi_data_scope_exists = 1,
  'ALTER TABLE `sys_openapi_client` DROP COLUMN `data_scope`',
  'SELECT 1'
);
PREPARE openapi_data_scope_stmt FROM @openapi_data_scope_sql;
EXECUTE openapi_data_scope_stmt;
DEALLOCATE PREPARE openapi_data_scope_stmt;
