-- Isolated test databases only. Never run on live authorization state.
IF OBJECT_ID(N'dbo.sys_openapi_audit', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_audit;
IF OBJECT_ID(N'dbo.sys_openapi_nonce', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_nonce;
IF OBJECT_ID(N'dbo.sys_openapi_client_scope', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_client_scope;
IF OBJECT_ID(N'dbo.sys_openapi_client', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_client;
