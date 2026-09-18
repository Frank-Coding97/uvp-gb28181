-- GB/T 28181 目录通道属性落库(附录 A / §9.3.1) —— SQL Server 方言
-- 与 2026-09-18-channel-catalog-attributes.sql 等价;0 / N'' 表示"设备未上报"。

IF COL_LENGTH(N'gb_channel', N'room_type') IS NULL
    ALTER TABLE [gb_channel] ADD [room_type] SMALLINT NOT NULL CONSTRAINT [df_gb_channel_room_type] DEFAULT 0;
IF COL_LENGTH(N'gb_channel', N'supply_light_type') IS NULL
    ALTER TABLE [gb_channel] ADD [supply_light_type] SMALLINT NOT NULL CONSTRAINT [df_gb_channel_supply_light_type] DEFAULT 0;
IF COL_LENGTH(N'gb_channel', N'direction_type') IS NULL
    ALTER TABLE [gb_channel] ADD [direction_type] SMALLINT NOT NULL CONSTRAINT [df_gb_channel_direction_type] DEFAULT 0;
IF COL_LENGTH(N'gb_channel', N'resolution') IS NULL
    ALTER TABLE [gb_channel] ADD [resolution] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_channel_resolution] DEFAULT N'';
IF COL_LENGTH(N'gb_channel', N'ip_address') IS NULL
    ALTER TABLE [gb_channel] ADD [ip_address] NVARCHAR(64) NOT NULL CONSTRAINT [df_gb_channel_ip_address] DEFAULT N'';
IF COL_LENGTH(N'gb_channel', N'port') IS NULL
    ALTER TABLE [gb_channel] ADD [port] INT NOT NULL CONSTRAINT [df_gb_channel_port] DEFAULT 0;

-- 版本独有属性:2016 的 PositionType/UseType 与 2022 的 PhotoelectricImagingType/
-- CapturePositionType。两组在 XSD 上互斥,是"设备用了哪一版目录形态"的直接证据。
IF COL_LENGTH(N'gb_channel', N'position_type') IS NULL
    ALTER TABLE [gb_channel] ADD [position_type] SMALLINT NOT NULL CONSTRAINT [df_gb_channel_position_type] DEFAULT 0;
IF COL_LENGTH(N'gb_channel', N'use_type') IS NULL
    ALTER TABLE [gb_channel] ADD [use_type] SMALLINT NOT NULL CONSTRAINT [df_gb_channel_use_type] DEFAULT 0;
IF COL_LENGTH(N'gb_channel', N'photoelectric_imaging_type') IS NULL
    ALTER TABLE [gb_channel] ADD [photoelectric_imaging_type] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_channel_photoelectric_imaging_type] DEFAULT N'';
IF COL_LENGTH(N'gb_channel', N'capture_position_type') IS NULL
    ALTER TABLE [gb_channel] ADD [capture_position_type] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_channel_capture_position_type] DEFAULT N'';

-- ptz_type 的列注释同步到 2022 值域(1-7)。SQL Server 走扩展属性。
IF NOT EXISTS (
    SELECT 1 FROM sys.extended_properties
    WHERE [name] = N'MS_Description' AND major_id = OBJECT_ID(N'gb_channel')
      AND minor_id = (SELECT column_id FROM sys.columns WHERE [object_id] = OBJECT_ID(N'gb_channel') AND [name] = N'ptz_type')
)
    EXEC sp_addextendedproperty
        @name = N'MS_Description',
        @value = N'云台类型 0未知 1球机 2半球 3固定枪机 4遥控枪机 5遥控半球 6多目全景/拼接通道 7多目分割通道(5-7 为 2022 新增)',
        @level0type = N'SCHEMA', @level0name = N'dbo',
        @level1type = N'TABLE',  @level1name = N'gb_channel',
        @level2type = N'COLUMN', @level2name = N'ptz_type';

-- ptz_type 字典项补齐到 2022 值域(5/6/7),依据 GB/T 28181-2022 附录 A。
DECLARE @ptz_type_dict_id BIGINT;
SELECT TOP (1) @ptz_type_dict_id = [id]
FROM [sys_dict]
WHERE [code] = 'ptz_type' AND [deleted_at] IS NULL
ORDER BY [id];

IF @ptz_type_dict_id IS NOT NULL
BEGIN
    UPDATE [sys_dict]
    SET [description] = N'GB28181 国标摄像头云台类型(PTZType),2022 值域 1-7', [updated_at] = CURRENT_TIMESTAMP
    WHERE [id] = @ptz_type_dict_id;

    IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id] = @ptz_type_dict_id AND [value] = '5')
        INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'遥控半球','5',1,@ptz_type_dict_id);
    IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id] = @ptz_type_dict_id AND [value] = '6')
        INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'多目设备的全景/拼接通道','6',1,@ptz_type_dict_id);
    IF NOT EXISTS (SELECT 1 FROM [sys_dict_item] WHERE [dict_id] = @ptz_type_dict_id AND [value] = '7')
        INSERT INTO [sys_dict_item] ([name],[value],[status],[dict_id]) VALUES (N'多目设备的分割通道','7',1,@ptz_type_dict_id);
END;
