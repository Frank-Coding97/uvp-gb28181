-- GB/T 28181 配置家族（A.2.4.7 ConfigDownload 查询 / A.2.3.2.5 DeviceConfig 下发）。
--
-- 1) 落库表 gb_device_config：**每 (设备, 目标编码, 配置类型) 一行**，
--    存该组配置最近一次回读得到的规范化 JSON（见 models.GbDeviceConfig）。
-- 2) 两个接口的权限绑定：读沿用 gb28181:ptz:view（与 video-params / storage-cards /
--    device-status 同族：都是"看设备事实"），写绑定 gb28181:ptz:control（写入有副作用，
--    会真的改设备配置）。
--
-- ⛔ 为什么是一张通用表而不是每组一张：配置家族 8 个 ConfigType 共用**同一条读写通道**，
--    差别只在块内字段。每组一张表 = 8 套迁移 × 3 方言 + 8 个模型 + 8 个 DDL 测试，
--    而换来的查询能力这个场景用不上（读取永远是"按三元组取一行"）。
--    组内字段的取值范围校验在协议层，**不在库里做约束**。
--
-- ⛔ 与 2026-09-18-channel-video-param 的分工：VideoParamAttribute 那一组按码流分条
--    （每码流一份配置），是真实的粒度差异，有专门的 gb_device_video_param；本表按
--    config_type 分条即可。
--
-- 幂等：建表用 IF NOT EXISTS；权限三件事（sys_api / sys_menu_api / sys_casbin_rule）
--       全部 NOT EXISTS 守卫。

CREATE TABLE IF NOT EXISTS `gb_device_config` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `target_code` varchar(20) NOT NULL,
  `config_type` varchar(32) NOT NULL,
  `payload_json` text NOT NULL,
  `source_operation_seq` bigint unsigned NOT NULL DEFAULT 0,
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `observed_at` datetime(3) NOT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_device_config_target` (`device_id`,`target_code`,`config_type`),
  KEY `idx_device_config_device` (`device_id`,`observed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- device-config-family-permissions:start
-- 以下为纯 DML（权限三件事），不含方言特有的 DDL 语法 —— 行为测试会在 sqlite 上
-- 连跑两遍验幂等、再跑 down 验无残留，所以这一段的边界用注释标出来供测试切分。

-- ---- 读接口：GET /api/gb28181/device-mgmt/channel/:id/device-configs ----
-- 不带 refresh：只读缓存；带 refresh=true：先发起一次 ConfigDownload 再返回当前缓存。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取设备配置','/api/gb28181/device-mgmt/channel/:id/device-configs','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/channel/:id/device-configs' AND p.v2='GET' AND p.v3='*');

-- ---- 写接口：POST /api/gb28181/device-mgmt/channel/:id/device-configs ----
-- ⛔ 写入有副作用（会真的改设备配置），所以权限比读高一档：绑 gb28181:ptz:control，
-- 不沿用读接口的 gb28181:ptz:view。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '下发设备配置','/api/gb28181/device-mgmt/channel/:id/device-configs','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/channel/:id/device-configs' AND p.v2='POST' AND p.v3='*');
