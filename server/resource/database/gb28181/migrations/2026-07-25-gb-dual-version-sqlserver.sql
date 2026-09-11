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
IF COL_LENGTH(N'gb_ptz_operation', N'scope_key') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [scope_key] NVARCHAR(64) NULL;

IF COL_LENGTH(N'gb_ptz_state', N'device_code') IS NULL
    ALTER TABLE [gb_ptz_state] ADD [device_code] NVARCHAR(20) NULL;

UPDATE state
SET [device_code] = COALESCE(NULLIF(state.[device_code], N''), NULLIF(device.[device_id], N''), N'')
FROM [gb_ptz_state] AS state
LEFT JOIN [gb_device] AS device ON device.[id] = state.[device_id]
WHERE state.[device_code] IS NULL OR state.[device_code] = N'';

IF EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'gb_ptz_state') AND name = N'device_code' AND is_nullable = 1
)
    ALTER TABLE [gb_ptz_state] ALTER COLUMN [device_code] NVARCHAR(20) NOT NULL;

IF OBJECT_ID(N'gb_ptz_cruise_track', N'U') IS NOT NULL
   AND COL_LENGTH(N'gb_ptz_cruise_track', N'last_operation_id') IS NULL
    ALTER TABLE [gb_ptz_cruise_track] ADD [last_operation_id] NVARCHAR(64) NULL;

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
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_control_state_source_operation_seq] DEFAULT 0,
        [raw_summary] NVARCHAR(MAX) NULL,
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_gb_device_control_state] PRIMARY KEY ([id]),
        CONSTRAINT [uk_control_state_target] UNIQUE ([device_id], [target_scope], [target_code])
    );
END;

IF COL_LENGTH(N'gb_device_control_state', N'source_operation_seq') IS NULL
    ALTER TABLE [gb_device_control_state] ADD [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_control_state_source_operation_seq] DEFAULT 0;

-- Older SQL Server full schemas used a globally unique target constraint. It
-- must be removed before the device-scoped key is installed below; otherwise
-- two devices using the same target code remain incorrectly blocked.
IF EXISTS (
    SELECT 1
    FROM sys.key_constraints
    WHERE parent_object_id = OBJECT_ID(N'gb_device_control_state')
      AND name = N'uk_full_control_state_target'
)
    ALTER TABLE [gb_device_control_state] DROP CONSTRAINT [uk_full_control_state_target];
IF EXISTS (
    SELECT 1
    FROM sys.indexes
    WHERE object_id = OBJECT_ID(N'gb_device_control_state')
      AND name = N'uk_full_control_state_target'
)
    DROP INDEX [uk_full_control_state_target] ON [gb_device_control_state];

-- GORM-created legacy databases may have the old key as a standalone unique
-- index rather than a key constraint. Remove that index before adding the
-- canonical constraint below; otherwise the global target uniqueness remains.
IF EXISTS (
    SELECT 1
    FROM sys.indexes ix
    WHERE ix.object_id = OBJECT_ID(N'gb_device_control_state')
      AND ix.name = N'uk_control_state_target'
      AND NOT EXISTS (
          SELECT 1
          FROM sys.key_constraints kc
          WHERE kc.parent_object_id = ix.object_id
            AND kc.unique_index_id = ix.index_id
            AND kc.name = N'uk_control_state_target'
      )
)
    DROP INDEX [uk_control_state_target] ON [gb_device_control_state];

DECLARE @control_state_unique_matches BIT = 0;
IF EXISTS (
    SELECT 1
    FROM sys.key_constraints kc
    JOIN sys.index_columns ic1 ON ic1.object_id = kc.parent_object_id AND ic1.index_id = kc.unique_index_id AND ic1.key_ordinal = 1
    JOIN sys.columns c1 ON c1.object_id = ic1.object_id AND c1.column_id = ic1.column_id
    JOIN sys.index_columns ic2 ON ic2.object_id = kc.parent_object_id AND ic2.index_id = kc.unique_index_id AND ic2.key_ordinal = 2
    JOIN sys.columns c2 ON c2.object_id = ic2.object_id AND c2.column_id = ic2.column_id
    JOIN sys.index_columns ic3 ON ic3.object_id = kc.parent_object_id AND ic3.index_id = kc.unique_index_id AND ic3.key_ordinal = 3
    JOIN sys.columns c3 ON c3.object_id = ic3.object_id AND c3.column_id = ic3.column_id
    WHERE kc.parent_object_id = OBJECT_ID(N'gb_device_control_state')
      AND kc.name = N'uk_control_state_target'
      AND c1.name = N'device_id' AND c2.name = N'target_scope' AND c3.name = N'target_code'
      AND (SELECT COUNT(*) FROM sys.index_columns ic
           WHERE ic.object_id = kc.parent_object_id AND ic.index_id = kc.unique_index_id AND ic.key_ordinal > 0) = 3
)
    SET @control_state_unique_matches = 1;

IF EXISTS (SELECT 1 FROM sys.key_constraints WHERE parent_object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'uk_control_state_target')
   AND @control_state_unique_matches = 0
    ALTER TABLE [gb_device_control_state] DROP CONSTRAINT [uk_control_state_target];
IF NOT EXISTS (SELECT 1 FROM sys.key_constraints WHERE parent_object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'uk_control_state_target')
    ALTER TABLE [gb_device_control_state] ADD CONSTRAINT [uk_control_state_target] UNIQUE ([device_id], [target_scope], [target_code]);

-- The historical SQL Server full schema used prefixed non-unique indexes.
-- Remove those aliases before installing the canonical names below so an
-- upgrade does not leave duplicate write overhead behind.
IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_full_control_state_device_target' AND is_unique_constraint = 0 AND is_primary_key = 0)
    DROP INDEX [idx_full_control_state_device_target] ON [gb_device_control_state];
IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_full_control_state_channel' AND is_unique_constraint = 0 AND is_primary_key = 0)
    DROP INDEX [idx_full_control_state_channel] ON [gb_device_control_state];
IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_full_ptz_operation_target' AND is_unique_constraint = 0 AND is_primary_key = 0)
    DROP INDEX [idx_full_ptz_operation_target] ON [gb_ptz_operation];

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_device_target')
    CREATE INDEX [idx_control_state_device_target] ON [gb_device_control_state] ([device_id], [target_scope], [target_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_channel')
    CREATE INDEX [idx_control_state_channel] ON [gb_device_control_state] ([channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_target')
    CREATE INDEX [idx_ptz_operation_target] ON [gb_ptz_operation] ([device_code], [target_scope], [target_code], [status]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_device_scope_time')
    CREATE INDEX [idx_ptz_operation_device_scope_time] ON [gb_ptz_operation] ([device_id], [scope_key], [created_at]);

UPDATE [gb_device]
SET [protocol_override] = CASE WHEN [protocol_override] IS NULL OR LTRIM(RTRIM([protocol_override])) = N'' THEN N'auto' ELSE [protocol_override] END,
    [effective_version] = CASE WHEN [effective_version] IS NULL OR LTRIM(RTRIM([effective_version])) = N'' THEN N'2016' ELSE [effective_version] END,
    [effective_version_source] = CASE WHEN [effective_version_source] IS NULL OR LTRIM(RTRIM([effective_version_source])) = N'' THEN N'default' ELSE [effective_version_source] END;

UPDATE [gb_ptz_operation]
SET [profile_version] = COALESCE(NULLIF(LTRIM(RTRIM([profile_version])), N''), N'2016'),
    [profile_charset] = COALESCE(NULLIF(LTRIM(RTRIM([profile_charset])), N''), N'GB2312'),
    [target_scope] = COALESCE(NULLIF(LTRIM(RTRIM([target_scope])), N''), N'channel'),
    [target_code] = COALESCE(NULLIF(LTRIM(RTRIM([target_code])), N''), [channel_code]),
    [scope_key] = COALESCE(NULLIF(LTRIM(RTRIM([scope_key])), N''), COALESCE(NULLIF(LTRIM(RTRIM([target_scope])), N''), N'channel') + N':' + COALESCE(NULLIF(LTRIM(RTRIM([target_code])), N''), [channel_code]))
WHERE [profile_version] IS NULL OR [profile_charset] IS NULL OR [target_scope] IS NULL OR [target_code] IS NULL OR [scope_key] IS NULL
   OR LTRIM(RTRIM([profile_version])) = N'' OR LTRIM(RTRIM([profile_charset])) = N''
   OR LTRIM(RTRIM([target_scope])) = N'' OR LTRIM(RTRIM([target_code])) = N'' OR LTRIM(RTRIM([scope_key])) = N'';
