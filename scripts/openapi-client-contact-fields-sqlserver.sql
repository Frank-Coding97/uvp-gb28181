-- OpenAPI 客户端外部负责人字段增量升级。
-- 适用于已存在 sys_openapi_client 的 SQL Server 存量库；可重复执行。
IF COL_LENGTH(N'dbo.sys_openapi_client', N'responsible_org_name') IS NULL
  ALTER TABLE [sys_openapi_client] ADD [responsible_org_name] NVARCHAR(200) COLLATE Latin1_General_100_BIN2 NOT NULL CONSTRAINT [df_openapi_client_responsible_org_name] DEFAULT N'';
IF COL_LENGTH(N'dbo.sys_openapi_client', N'responsible_name') IS NULL
  ALTER TABLE [sys_openapi_client] ADD [responsible_name] NVARCHAR(100) COLLATE Latin1_General_100_BIN2 NOT NULL CONSTRAINT [df_openapi_client_responsible_name] DEFAULT N'';
IF COL_LENGTH(N'dbo.sys_openapi_client', N'responsible_contact') IS NULL
  ALTER TABLE [sys_openapi_client] ADD [responsible_contact] NVARCHAR(100) COLLATE Latin1_General_100_BIN2 NOT NULL CONSTRAINT [df_openapi_client_responsible_contact] DEFAULT N'';
