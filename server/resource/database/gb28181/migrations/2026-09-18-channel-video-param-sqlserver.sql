-- GB/T 28181-2022 A.2.1.13 / A.2.3.2.5「视频参数属性」(VideoParamAttribute)
-- —— SQL Server 方言，与 2026-09-18-channel-video-param.sql 等价。
--
-- 1) 落库表 gb_device_video_param：**每码流一行**（见 models.GbDeviceVideoParam）。
-- 2) gb_channel 补 stream_number_list 列：面板的码流分段数出处。
--    ⛔ 出处是目录 <Info> 里的 StreamNumberList（A.2.1.9，2022 新增），
--    **不是** VideoParamOpt —— 后者只有 DownloadSpeed + Resolution 两个字段。
-- 3) 两个接口的权限绑定：读沿用 gb28181:ptz:view，写绑定 gb28181:ptz:control。
--
-- 约定：五个取值列**原样存附录 G 的码值字符串**，人读串只在前端做。
--       video_bit_rate 可空 —— NULL 表示"这一帧设备没给这个元素"（VBR 时本就该缺席）。

IF OBJECT_ID(N'gb_device_video_param', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_video_param] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [target_code] NVARCHAR(20) NOT NULL,
        [stream_number] INT NOT NULL,
        [video_format] NVARCHAR(8) NOT NULL CONSTRAINT [df_video_param_video_format] DEFAULT N'',
        [resolution] NVARCHAR(32) NOT NULL CONSTRAINT [df_video_param_resolution] DEFAULT N'',
        [frame_rate] NVARCHAR(8) NOT NULL CONSTRAINT [df_video_param_frame_rate] DEFAULT N'',
        [bit_rate_type] NVARCHAR(8) NOT NULL CONSTRAINT [df_video_param_bit_rate_type] DEFAULT N'',
        [video_bit_rate] NVARCHAR(16),
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_video_param_source_seq] DEFAULT 0,
        [source_sn] INT NOT NULL CONSTRAINT [df_video_param_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64),
        [observed_at] DATETIME2(3) NOT NULL,
        [raw_summary] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_video_param] PRIMARY KEY ([id]),
        CONSTRAINT [uk_video_param_target] UNIQUE ([device_id], [target_code], [stream_number])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_video_param') AND name = N'uk_video_param_target')
    CREATE UNIQUE INDEX [uk_video_param_target] ON [gb_device_video_param] ([device_id], [target_code], [stream_number]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_video_param') AND name = N'idx_video_param_device')
    CREATE INDEX [idx_video_param_device] ON [gb_device_video_param] ([device_id], [observed_at]);

-- ---- gb_channel.stream_number_list ----
-- ⛔ 与 2026-09-18-channel-catalog-attributes 那批同属 <Info> 容器内的属性。
IF COL_LENGTH(N'gb_channel', N'stream_number_list') IS NULL
    ALTER TABLE [gb_channel] ADD [stream_number_list] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_channel_stream_number_list] DEFAULT N'';

IF NOT EXISTS (
    SELECT 1 FROM sys.extended_properties
    WHERE [name] = N'MS_Description' AND major_id = OBJECT_ID(N'gb_channel')
      AND minor_id = (SELECT column_id FROM sys.columns WHERE [object_id] = OBJECT_ID(N'gb_channel') AND [name] = N'stream_number_list')
)
    EXEC sp_addextendedproperty
        @name = N'MS_Description',
        @value = N'支持的码流编号 2022独有,可多值 / 分隔,如 0/1 或 0/1/2',
        @level0type = N'SCHEMA', @level0name = N'dbo',
        @level1type = N'TABLE',  @level1name = N'gb_channel',
        @level2type = N'COLUMN', @level2name = N'stream_number_list';

-- ---- 读接口：GET /api/gb28181/device-mgmt/channel/:id/video-params ----
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'读取视频参数',N'/api/gb28181/device-mgmt/channel/:id/video-params',N'GET',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND method=N'GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');

-- ---- 写接口：POST /api/gb28181/device-mgmt/channel/:id/video-params ----
-- ⛔ 写入有副作用（会真的改设备配置），所以权限比读高一档：绑 gb28181:ptz:control。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'下发视频参数',N'/api/gb28181/device-mgmt/channel/:id/video-params',N'POST',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND method=N'POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
