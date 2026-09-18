-- GB/T 28181-2022 A.2.4.14 / A.2.6.16「存储卡状态查询」。
-- 1) 落库表 gb_device_storage_card：每张卡一行（见 models.GbDeviceStorageCard）
-- 2) 查询接口 /channel/:id/storage-cards 的权限绑定，沿用 gb28181:ptz:view
--    （与既有 device-status / cruise-tracks 同族：都是"看设备事实"的读接口）
CREATE TABLE IF NOT EXISTS `gb_device_storage_card` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `target_code` varchar(20) NOT NULL,
  `card_id` int NOT NULL,
  `hdd_name` varchar(128) NOT NULL DEFAULT '',
  `status` varchar(16) NOT NULL DEFAULT 'unknown',
  `format_progress` int DEFAULT NULL,
  `capacity_mb` int NOT NULL DEFAULT 0,
  `free_space_mb` int NOT NULL DEFAULT 0,
  `source_operation_seq` bigint unsigned NOT NULL DEFAULT 0,
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `observed_at` datetime(3) NOT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_storage_card_target` (`device_id`,`target_code`,`card_id`),
  KEY `idx_storage_card_device` (`device_id`,`observed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询存储卡状态','/api/gb28181/device-mgmt/channel/:id/storage-cards','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
