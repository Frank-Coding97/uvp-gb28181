-- GB/T 28181-2022 A.2.1.13 / A.2.3.2.5「视频参数属性」(VideoParamAttribute)。
--
-- 1) 落库表 gb_device_video_param：**每码流一行**（见 models.GbDeviceVideoParam）。
--    一次 ConfigDownload 回读 = 每个码流一份配置，所以"每码流一行"才是可对账的最小粒度。
-- 2) gb_channel 补 stream_number_list 列：面板的码流分段数出处。
--    ⛔ 出处是目录 <Info> 里的 StreamNumberList（A.2.1.9，2022 新增），
--    **不是** VideoParamOpt —— 后者只有 DownloadSpeed + Resolution 两个字段。
--    B-2 那批十列当时判定它属"点播取流能力"而刻意没落库；现在它有了第二个
--    消费者（视频参数面板按码流分段），那句判定的前提已变，故补落。
-- 3) 两个接口的权限绑定：读沿用 gb28181:ptz:view（与 storage-cards / device-status
--    同族：都是"看设备事实"），写绑定 gb28181:ptz:control（与 preset/cruise 那族的
--    读/写分工一致）。
--
-- 约定：五个取值列**原样存附录 G 的码值字符串**，人读串只在前端做。
--       video_bit_rate 可空 —— NULL 表示"这一帧设备没给这个元素"（VBR 时本就该缺席），
--       与"设备给了个 0"是两件事。
--
-- 幂等：建表用 IF NOT EXISTS；加列先查 information_schema 再决定是否执行。

CREATE TABLE IF NOT EXISTS `gb_device_video_param` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `target_code` varchar(20) NOT NULL,
  `stream_number` int NOT NULL,
  `video_format` varchar(8) NOT NULL DEFAULT '',
  `resolution` varchar(32) NOT NULL DEFAULT '',
  `frame_rate` varchar(8) NOT NULL DEFAULT '',
  `bit_rate_type` varchar(8) NOT NULL DEFAULT '',
  `video_bit_rate` varchar(16) DEFAULT NULL,
  `source_operation_seq` bigint unsigned NOT NULL DEFAULT 0,
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `observed_at` datetime(3) NOT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_video_param_target` (`device_id`,`target_code`,`stream_number`),
  KEY `idx_video_param_device` (`device_id`,`observed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---- gb_channel.stream_number_list ----
-- ⛔ 追加在 capture_position_type 之后，与 2026-09-18-channel-catalog-attributes 那批
-- 同属 <Info> 容器内的属性；守卫式迁移无法重排**已存在**的列，所以顺序上跟着上一批走。
SET @ch_stream_number_list_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'stream_number_list'
);
SET @ch_stream_number_list_sql := IF(
  @ch_stream_number_list_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `stream_number_list` varchar(32) NOT NULL DEFAULT '''' COMMENT ''支持的码流编号 2022独有,可多值 / 分隔,如 0/1 或 0/1/2'' AFTER `capture_position_type`',
  'SELECT 1'
);
PREPARE ch_stream_number_list_stmt FROM @ch_stream_number_list_sql;
EXECUTE ch_stream_number_list_stmt;
DEALLOCATE PREPARE ch_stream_number_list_stmt;

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
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');

-- ---- 写接口：POST /api/gb28181/device-mgmt/channel/:id/video-params ----
-- ⛔ 写入有副作用（会真的改设备配置），所以权限比读高一档：绑 gb28181:ptz:control，
-- 不沿用读接口的 gb28181:ptz:view。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '下发视频参数','/api/gb28181/device-mgmt/channel/:id/video-params','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/video-params' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/video-params' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
