-- 回滚 OpenAPI 客户端数据范围字段。
-- 仅用于已确认的隔离环境；删除该列会丢弃每个客户端的范围选择。

ALTER TABLE sys_openapi_client
    DROP CONSTRAINT IF EXISTS ck_openapi_client_data_scope;
ALTER TABLE sys_openapi_client
    DROP COLUMN IF EXISTS data_scope;
