-- GB/T 28181-2022 A.2.1.13 / A.2.3.2.5「视频参数属性」(VideoParamAttribute)
-- —— PostgreSQL 方言，与 2026-09-18-channel-video-param.sql 等价。
--
-- 1) 落库表 gb_device_video_param：**每码流一行**（见 models.GbDeviceVideoParam）。
-- 2) gb_channel 补 stream_number_list 列：面板的码流分段数出处。
--    ⛔ 出处是目录 <Info> 里的 StreamNumberList（A.2.1.9，2022 新增），
--    **不是** VideoParamOpt —— 后者只有 DownloadSpeed + Resolution 两个字段。
-- 3) 两个接口的权限绑定：读沿用 gb28181:ptz:view，写绑定 gb28181:ptz:control。
--
-- 约定：五个取值列**原样存附录 G 的码值字符串**，人读串只在前端做。
--       video_bit_rate 可空 —— NULL 表示"这一帧设备没给这个元素"（VBR 时本就该缺席）。

CREATE TABLE IF NOT EXISTS gb_device_video_param (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    target_code VARCHAR(20) NOT NULL,
    stream_number INTEGER NOT NULL,
    video_format VARCHAR(8) NOT NULL DEFAULT '',
    resolution VARCHAR(32) NOT NULL DEFAULT '',
    frame_rate VARCHAR(8) NOT NULL DEFAULT '',
    bit_rate_type VARCHAR(8) NOT NULL DEFAULT '',
    video_bit_rate VARCHAR(16),
    source_operation_seq BIGINT NOT NULL DEFAULT 0,
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    observed_at TIMESTAMP(3) NOT NULL,
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_video_param_target UNIQUE (device_id, target_code, stream_number)
);

CREATE INDEX IF NOT EXISTS idx_video_param_device ON gb_device_video_param (device_id, observed_at);

-- ---- gb_channel.stream_number_list ----
-- ⛔ 追加在 capture_position_type 之后，与 2026-09-18-channel-catalog-attributes 那批
-- 同属 <Info> 容器内的属性；守卫式迁移无法重排**已存在**的列，所以顺序上跟着上一批走。
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS stream_number_list VARCHAR(32) NOT NULL DEFAULT '';

COMMENT ON COLUMN gb_channel.stream_number_list IS '支持的码流编号 2022独有,可多值 / 分隔,如 0/1 或 0/1/2';

-- ---- 读接口：GET /api/gb28181/device-mgmt/channel/:id/video-params ----
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取视频参数','/api/gb28181/device-mgmt/channel/:id/video-params','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1=a.path AND p.v2=a.method AND p.v3='*');

-- ---- 写接口：POST /api/gb28181/device-mgmt/channel/:id/video-params ----
-- ⛔ 写入有副作用（会真的改设备配置），所以权限比读高一档：绑 gb28181:ptz:control。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '下发视频参数','/api/gb28181/device-mgmt/channel/:id/video-params','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
