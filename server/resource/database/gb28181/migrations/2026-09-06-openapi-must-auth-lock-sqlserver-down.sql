-- Isolated empty test databases only, through the Go Down safety guard.
-- A latched commitment is never eligible for rollback or implicit unlock.
IF OBJECT_ID(N'dbo.sys_openapi_security_state', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_security_state;
