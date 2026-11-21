-- 录像缓存任务（设备录像缓存到服务器）
-- Migration: 2026-10-03-record-cache
--
-- ⚠️ 本文件只用于把新物件补进**已有的开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 已包含同样内容）。
--    与 2026-10-02-firmware-repository-menu.sql 同一形态：仅 MySQL 方言、可重复执行。
-- ⛔ 建表必须显式写 COLLATE=utf8mb4_general_ci，否则与既有表 JOIN 会报 1267。

CREATE TABLE IF NOT EXISTS `gb_record_cache_task` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `task_id` varchar(64) NOT NULL,
  `owner_dept_id` bigint unsigned NOT NULL DEFAULT 0,
  `created_by_user` int unsigned NOT NULL DEFAULT 0,
  `created_by_name` varchar(64) NOT NULL DEFAULT '',
  `channel_id` int unsigned NOT NULL,
  `device_id` varchar(20) NOT NULL,
  `channel_code` varchar(20) NOT NULL DEFAULT '',
  `channel_name` varchar(255) NOT NULL DEFAULT '',
  `device_name` varchar(255) NOT NULL DEFAULT '',
  `start_time` datetime NOT NULL,
  `end_time` datetime NOT NULL,
  `record_type` varchar(16) NOT NULL DEFAULT 'all',
  `download_speed` int NOT NULL DEFAULT 4,
  `node_id` bigint NOT NULL DEFAULT 0,
  `vhost` varchar(128) NOT NULL DEFAULT '__defaultVhost__',
  `app` varchar(64) NOT NULL DEFAULT 'rtp',
  `stream` varchar(64) NOT NULL DEFAULT '',
  `session_id` bigint unsigned NULL,
  `file_id` bigint unsigned NULL,
  `file_name` varchar(255) NOT NULL DEFAULT '',
  `file_path` varchar(1000) NOT NULL DEFAULT '',
  `file_size` bigint unsigned NOT NULL DEFAULT 0,
  `cached_bytes` bigint unsigned NOT NULL DEFAULT 0,
  `estimated_bytes` bigint unsigned NOT NULL DEFAULT 0,
  `state` varchar(20) NOT NULL,
  `last_error` varchar(500) NOT NULL DEFAULT '',
  `request_id` varchar(64) NOT NULL DEFAULT '',
  `started_at` datetime NULL,
  `finished_at` datetime NULL,
  `expires_at` datetime NULL,
  `created_at` datetime NULL,
  `updated_at` datetime NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_record_cache_task_id` (`task_id`),
  KEY `idx_record_cache_task_state_created` (`state`,`created_at`),
  KEY `idx_record_cache_task_channel_state` (`channel_id`,`state`),
  KEY `idx_record_cache_task_dept_state` (`owner_dept_id`,`state`),
  KEY `idx_record_cache_task_expires` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='设备录像缓存任务（拉流缓存到服务器）';

-- sys_menu: 录像缓存菜单项（与「云端录像」同层，sort=52 紧邻它）
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140610, 0, '/gb28181/record-cache', 'gb28181-record-cache', '', 'gb28181/record-cache/index', '录像缓存', 0, 0, 0, 0, 0, '', 0, '', 'lucide:HardDriveDownload', 52, 2, 0, 'gb28181:record-cache:view', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140610);

-- sys_menu: 录像缓存权限按钮
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140611, 140610, '', '', NULL, '', '创建缓存', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'gb28181:record-cache:create', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140611);

INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140612, 140610, '', '', NULL, '', '取消缓存', 0, 0, 0, 1, 0, '', 0, '', '', 2, 3, 0, 'gb28181:record-cache:cancel', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140612);

INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140613, 140610, '', '', NULL, '', '下载录像', 0, 0, 0, 1, 0, '', 0, '', '', 3, 3, 0, 'gb28181:record-cache:download', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140613);

INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140614, 140610, '', '', NULL, '', '删除缓存', 0, 0, 0, 1, 0, '', 0, '', '', 4, 3, 0, 'gb28181:record-cache:delete', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140614);

-- sys_api: 录像缓存接口（6 个）
INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 629, '创建设备录像缓存任务', '/api/gb28181/record-cache/tasks', 'POST', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 629);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 630, '查询设备录像缓存任务列表', '/api/gb28181/record-cache/tasks', 'GET', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 630);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 631, '查询设备录像缓存任务详情', '/api/gb28181/record-cache/tasks/:taskId', 'GET', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 631);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 632, '取消设备录像缓存任务', '/api/gb28181/record-cache/tasks/:taskId/cancel', 'POST', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 632);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 633, '删除设备录像缓存任务', '/api/gb28181/record-cache/tasks/:taskId', 'DELETE', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 633);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 634, '下载设备录像缓存文件', '/api/gb28181/record-cache/tasks/:taskId/content', 'GET', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 634);

-- sys_menu_api: 菜单与 API 关联。
-- 注意 629/630/631 额外挂到 140442（设备录像回放页的「设备录像下载」权限点）：
-- 缓存任务是在回放页点「缓存到服务器」发起的，只给 record-cache 菜单的用户会看不到入口。
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 629 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 629);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 630 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 630);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 631 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 631);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 632 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 632);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 633 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 633);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 634 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 634);

INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140442, 629 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140442 AND `api_id` = 629);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140442, 630 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140442 AND `api_id` = 630);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140442, 631 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140442 AND `api_id` = 631);

-- sys_role_menu: 管理员角色可见
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140610 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140610);
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140611 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140611);
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140612 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140612);
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140613 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140613);
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140614 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140614);

-- sys_casbin_rule: 管理员角色授权（policy 只增不删）
INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks', 'POST', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks' AND `v2` = 'POST');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks', 'GET', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks' AND `v2` = 'GET');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId', 'GET', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId' AND `v2` = 'GET');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId/cancel', 'POST', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId/cancel' AND `v2` = 'POST');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId', 'DELETE', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId' AND `v2` = 'DELETE');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId/content', 'GET', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId/content' AND `v2` = 'GET');

-- ⛔ down 不可自动执行：删除菜单/权限是 fail-closed 操作，需人工确认后再跑。
-- 如需回滚（仅开发库），按 permission 定位删按钮、按 id 删菜单，最后 DROP TABLE gb_record_cache_task。
