-- 2026-07-25: GB28181 2016/2022 profile archive and control-state facts.
-- SQL Server 2017+; every object/column is guarded for repeatable upgrades.

IF COL_LENGTH(N'gb_device', N'reported_version') IS NULL
    ALTER TABLE [gb_device] ADD [reported_version] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_reported_version] DEFAULT N'';
IF COL_LENGTH(N'gb_device', N'reported_version_at') IS NULL
    ALTER TABLE [gb_device] ADD [reported_version_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_device', N'protocol_override') IS NULL
    ALTER TABLE [gb_device] ADD [protocol_override] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_protocol_override] DEFAULT N'auto';
IF COL_LENGTH(N'gb_device', N'effective_version') IS NULL
    ALTER TABLE [gb_device] ADD [effective_version] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_effective_version] DEFAULT N'2016';
IF COL_LENGTH(N'gb_device', N'effective_version_source') IS NULL
    ALTER TABLE [gb_device] ADD [effective_version_source] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_device_effective_source] DEFAULT N'default';
IF COL_LENGTH(N'gb_device', N'effective_version_at') IS NULL
    ALTER TABLE [gb_device] ADD [effective_version_at] DATETIME2(3) NULL;

IF COL_LENGTH(N'gb_ptz_operation', N'profile_version') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [profile_version] NVARCHAR(8) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'profile_charset') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [profile_charset] NVARCHAR(16) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'target_scope') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [target_scope] NVARCHAR(16) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'target_code') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [target_code] NVARCHAR(20) NULL;

IF OBJECT_ID(N'gb_device_control_state', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_control_state] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL CONSTRAINT [df_control_state_channel] DEFAULT 0,
        [target_scope] NVARCHAR(16) NOT NULL,
        [target_code] NVARCHAR(20) NOT NULL,
        [record_state] NVARCHAR(8) NOT NULL CONSTRAINT [df_control_state_record] DEFAULT N'unknown',
        [guard_state] NVARCHAR(8) NOT NULL CONSTRAINT [df_control_state_guard] DEFAULT N'unknown',
        [freshness] NVARCHAR(8) NOT NULL CONSTRAINT [df_control_state_freshness] DEFAULT N'unknown',
        [observed_at] DATETIME2(3) NOT NULL,
        [source] NVARCHAR(32) NOT NULL CONSTRAINT [df_control_state_source] DEFAULT N'device_status',
        [source_sn] INT NOT NULL CONSTRAINT [df_control_state_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64) NULL,
        [raw_summary] NVARCHAR(MAX) NULL,
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_gb_device_control_state] PRIMARY KEY ([id]),
        CONSTRAINT [uk_control_state_target] UNIQUE ([target_scope], [target_code])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_device_target')
    CREATE INDEX [idx_control_state_device_target] ON [gb_device_control_state] ([device_id], [target_scope], [target_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_channel')
    CREATE INDEX [idx_control_state_channel] ON [gb_device_control_state] ([channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_target')
    CREATE INDEX [idx_ptz_operation_target] ON [gb_ptz_operation] ([device_code], [target_scope], [target_code], [status]);

UPDATE [gb_device]
SET [protocol_override] = CASE WHEN [protocol_override] IS NULL OR LTRIM(RTRIM([protocol_override])) = N'' THEN N'auto' ELSE [protocol_override] END,
    [effective_version] = CASE WHEN [effective_version] IS NULL OR LTRIM(RTRIM([effective_version])) = N'' THEN N'2016' ELSE [effective_version] END,
    [effective_version_source] = CASE WHEN [effective_version_source] IS NULL OR LTRIM(RTRIM([effective_version_source])) = N'' THEN N'default' ELSE [effective_version_source] END;

UPDATE [gb_ptz_operation]
SET [profile_version] = COALESCE(NULLIF(LTRIM(RTRIM([profile_version])), N''), N'2016'),
    [profile_charset] = COALESCE(NULLIF(LTRIM(RTRIM([profile_charset])), N''), N'GB2312'),
    [target_scope] = COALESCE(NULLIF(LTRIM(RTRIM([target_scope])), N''), N'channel'),
    [target_code] = COALESCE(NULLIF(LTRIM(RTRIM([target_code])), N''), [channel_code])
WHERE [profile_version] IS NULL OR [profile_charset] IS NULL OR [target_scope] IS NULL OR [target_code] IS NULL
   OR LTRIM(RTRIM([profile_version])) = N'' OR LTRIM(RTRIM([profile_charset])) = N''
   OR LTRIM(RTRIM([target_scope])) = N'' OR LTRIM(RTRIM([target_code])) = N'';
