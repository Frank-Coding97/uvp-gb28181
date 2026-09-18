-- 回滚 2026-09-18-channel-catalog-attributes-sqlserver.sql —— SQL Server 方言
--
-- ⛔ SQL Server 不允许直接 DROP 带默认值约束的列(报 "is dependent on column"),
--    必须先 DROP CONSTRAINT 再 DROP COLUMN。
-- ⛔ ptz_type 的扩展属性(MS_Description)变更不回滚(纯文档修正)。
-- ⚠️ 回滚会丢弃已落库的通道属性。

IF OBJECT_ID(N'df_gb_channel_room_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_room_type];
IF COL_LENGTH(N'gb_channel', N'room_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [room_type];

IF OBJECT_ID(N'df_gb_channel_supply_light_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_supply_light_type];
IF COL_LENGTH(N'gb_channel', N'supply_light_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [supply_light_type];

IF OBJECT_ID(N'df_gb_channel_direction_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_direction_type];
IF COL_LENGTH(N'gb_channel', N'direction_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [direction_type];

IF OBJECT_ID(N'df_gb_channel_resolution', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_resolution];
IF COL_LENGTH(N'gb_channel', N'resolution') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [resolution];

IF OBJECT_ID(N'df_gb_channel_ip_address', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_ip_address];
IF COL_LENGTH(N'gb_channel', N'ip_address') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [ip_address];

IF OBJECT_ID(N'df_gb_channel_port', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_port];
IF COL_LENGTH(N'gb_channel', N'port') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [port];

IF OBJECT_ID(N'df_gb_channel_position_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_position_type];
IF COL_LENGTH(N'gb_channel', N'position_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [position_type];

IF OBJECT_ID(N'df_gb_channel_use_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_use_type];
IF COL_LENGTH(N'gb_channel', N'use_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [use_type];

IF OBJECT_ID(N'df_gb_channel_photoelectric_imaging_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_photoelectric_imaging_type];
IF COL_LENGTH(N'gb_channel', N'photoelectric_imaging_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [photoelectric_imaging_type];

IF OBJECT_ID(N'df_gb_channel_capture_position_type', N'D') IS NOT NULL
    ALTER TABLE [gb_channel] DROP CONSTRAINT [df_gb_channel_capture_position_type];
IF COL_LENGTH(N'gb_channel', N'capture_position_type') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [capture_position_type];

-- 一并移除本次补齐的 ptz_type 字典项(5/6/7)。0-4 是历史项,保持不变。
DELETE i
FROM [sys_dict_item] i
JOIN [sys_dict] d ON d.[id] = i.[dict_id]
WHERE d.[code] = 'ptz_type' AND d.[deleted_at] IS NULL AND i.[value] IN ('5', '6', '7');
