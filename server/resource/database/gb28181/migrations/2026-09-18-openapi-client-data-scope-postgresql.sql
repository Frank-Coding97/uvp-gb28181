-- OpenAPI 客户端数据范围：3=本部门，4=本部门及以下。
-- 旧客户端统一按 3（本部门）处理，避免升级后扩大既有凭证的可见范围。

ALTER TABLE sys_openapi_client
    ADD COLUMN IF NOT EXISTS data_scope SMALLINT NOT NULL DEFAULT 3;

-- 兼容早期试运行版本或人工写入的非法值，先收敛到最小权限再加约束。
UPDATE sys_openapi_client
SET data_scope = 3
WHERE data_scope IS NULL OR data_scope NOT IN (3, 4);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'sys_openapi_client'::regclass
          AND conname = 'ck_openapi_client_data_scope'
    ) THEN
        ALTER TABLE sys_openapi_client
            ADD CONSTRAINT ck_openapi_client_data_scope CHECK (data_scope IN (3, 4));
    END IF;
END $$;
