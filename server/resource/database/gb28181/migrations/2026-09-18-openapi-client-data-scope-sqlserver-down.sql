-- 回滚 OpenAPI 客户端数据范围字段。
-- 仅用于已确认的隔离环境；删除该列会丢弃每个客户端的范围选择。

IF OBJECT_ID(N'dbo.ck_openapi_client_data_scope', N'C') IS NOT NULL
    ALTER TABLE dbo.sys_openapi_client DROP CONSTRAINT ck_openapi_client_data_scope;
IF OBJECT_ID(N'dbo.df_openapi_client_data_scope', N'D') IS NOT NULL
    ALTER TABLE dbo.sys_openapi_client DROP CONSTRAINT df_openapi_client_data_scope;
IF COL_LENGTH(N'dbo.sys_openapi_client', N'data_scope') IS NOT NULL
    ALTER TABLE dbo.sys_openapi_client DROP COLUMN data_scope;
