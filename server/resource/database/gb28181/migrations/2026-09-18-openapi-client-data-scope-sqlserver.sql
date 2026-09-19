-- OpenAPI 客户端数据范围：3=本部门，4=本部门及以下。
-- 旧客户端统一按 3（本部门）处理，避免升级后扩大既有凭证的可见范围。

IF COL_LENGTH(N'dbo.sys_openapi_client', N'data_scope') IS NULL
    ALTER TABLE dbo.sys_openapi_client
        ADD data_scope TINYINT NOT NULL
            CONSTRAINT df_openapi_client_data_scope DEFAULT 3 WITH VALUES;

-- 兼容早期试运行版本或人工写入的非法值，先收敛到最小权限再加约束。
UPDATE dbo.sys_openapi_client
SET data_scope = 3
WHERE data_scope IS NULL OR data_scope NOT IN (3, 4);

IF NOT EXISTS (
    SELECT 1
    FROM sys.check_constraints
    WHERE parent_object_id = OBJECT_ID(N'dbo.sys_openapi_client')
      AND name = N'ck_openapi_client_data_scope'
)
    ALTER TABLE dbo.sys_openapi_client
        ADD CONSTRAINT ck_openapi_client_data_scope CHECK (data_scope IN (3, 4));
