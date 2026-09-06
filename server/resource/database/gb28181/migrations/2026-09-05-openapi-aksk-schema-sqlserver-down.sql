-- Isolated empty test databases only. Never run on live authorization state or
-- use this file to delete business rows. The Down executor must verify the
-- media/device baseline before executing these feature-only inverse operations.
IF OBJECT_ID(N'dbo.gb_openapi_viewer', N'U') IS NOT NULL DROP TABLE dbo.gb_openapi_viewer;
IF OBJECT_ID(N'dbo.gb_openapi_play_grant', N'U') IS NOT NULL DROP TABLE dbo.gb_openapi_play_grant;

IF COL_LENGTH(N'dbo.meta_node', N'runtime_identity_status') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'dbo.df_meta_node_runtime_identity_status', N'D') IS NOT NULL ALTER TABLE dbo.meta_node DROP CONSTRAINT df_meta_node_runtime_identity_status;
    ALTER TABLE dbo.meta_node DROP COLUMN runtime_identity_status;
END;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_confirmed_at') IS NOT NULL ALTER TABLE dbo.meta_node DROP COLUMN runtime_confirmed_at;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_confirmed_revision') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'dbo.df_meta_node_runtime_confirmed_revision', N'D') IS NOT NULL ALTER TABLE dbo.meta_node DROP CONSTRAINT df_meta_node_runtime_confirmed_revision;
    ALTER TABLE dbo.meta_node DROP COLUMN runtime_confirmed_revision;
END;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_protocol_version') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'dbo.df_meta_node_runtime_protocol_version', N'D') IS NOT NULL ALTER TABLE dbo.meta_node DROP CONSTRAINT df_meta_node_runtime_protocol_version;
    ALTER TABLE dbo.meta_node DROP COLUMN runtime_protocol_version;
END;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_epoch') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'dbo.df_meta_node_runtime_epoch', N'D') IS NOT NULL ALTER TABLE dbo.meta_node DROP CONSTRAINT df_meta_node_runtime_epoch;
    ALTER TABLE dbo.meta_node DROP COLUMN runtime_epoch;
END;
IF COL_LENGTH(N'dbo.meta_node', N'retired_boot_history') IS NOT NULL ALTER TABLE dbo.meta_node DROP COLUMN retired_boot_history;
IF COL_LENGTH(N'dbo.meta_node', N'current_boot_nonce') IS NOT NULL ALTER TABLE dbo.meta_node DROP COLUMN current_boot_nonce;
IF COL_LENGTH(N'dbo.gb_device', N'legacy_revoked_before') IS NOT NULL ALTER TABLE dbo.gb_device DROP COLUMN legacy_revoked_before;
IF COL_LENGTH(N'dbo.gb_device', N'access_epoch') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'dbo.ck_gb_device_access_epoch', N'C') IS NOT NULL ALTER TABLE dbo.gb_device DROP CONSTRAINT ck_gb_device_access_epoch;
    IF OBJECT_ID(N'dbo.df_gb_device_access_epoch', N'D') IS NOT NULL ALTER TABLE dbo.gb_device DROP CONSTRAINT df_gb_device_access_epoch;
    ALTER TABLE dbo.gb_device DROP COLUMN access_epoch;
END;

IF OBJECT_ID(N'dbo.sys_openapi_audit', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_audit;
IF OBJECT_ID(N'dbo.sys_openapi_nonce', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_nonce;
IF OBJECT_ID(N'dbo.sys_openapi_client_scope', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_client_scope;
IF OBJECT_ID(N'dbo.sys_openapi_client', N'U') IS NOT NULL DROP TABLE dbo.sys_openapi_client;
