-- 录像缓存：收藏标记（收藏后不参与保留期自动清理）+ 收藏接口登记
-- Migration: 2026-10-05-record-cache-favorite
--
-- 背景：新增「收藏」能力。收藏的唯一语义是
--   **这条录像不再参与保留期自动清理**（cleanupExpired 的候选集在 SQL 上就过滤掉了），
--   手动删除不受影响 —— 收藏不是"防手滑"，用户显式删掉的仍然会删。
--
-- ⛔ 收藏标记**必须落库**，不能只在 Go 侧记内存：自动清理跑在后台 Tick 里，
--    内存标记拦不住它 —— 表现是"我明明收藏了，过几天文件还是没了"，而且没有任何报错。
--
-- ⚠️ 与 2026-10-03-record-cache*.sql 同形态：仅 MySQL 方言、可重复执行，
--    用于把新物件补进**已有开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 已包含同样内容）。

-- ---- favorite 列 ----
SET @table_exists := (
  SELECT COUNT(*) FROM information_schema.TABLES
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task'
);
SET @favorite_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_record_cache_task' AND COLUMN_NAME = 'favorite'
);
-- ⛔ MySQL 不支持 `ADD COLUMN IF NOT EXISTS`（那是 MariaDB 扩展），必须自己判存在性后动态执行。
--    写成裸 ADD COLUMN 时，第二次执行报 1060 Duplicate column name，会把整批迁移中断在半路。
SET @ddl := IF(@table_exists = 1 AND @favorite_exists = 0,
  'ALTER TABLE `gb_record_cache_task` ADD COLUMN `favorite` tinyint NOT NULL DEFAULT 0 COMMENT ''收藏标记（收藏后不参与保留期自动清理）'' AFTER `expires_at`',
  'SELECT 1');
PREPARE record_cache_favorite_stmt FROM @ddl;
EXECUTE record_cache_favorite_stmt;
DEALLOCATE PREPARE record_cache_favorite_stmt;

-- sys_menu: 「收藏缓存」按钮权限点（挂在录像缓存菜单 140610 下）
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140615, 140610, '', '', NULL, '', '收藏缓存', 0, 0, 0, 1, 0, '', 0, '', '', 5, 3, 0, 'gb28181:record-cache:favorite', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140615);

-- sys_role_menu: 管理员角色可见
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, 140615 WHERE NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id` = 1 AND `menu_id` = 140615);

-- sys_api: 收藏接口
INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 638, '收藏设备录像缓存任务', '/api/gb28181/record-cache/tasks/:taskId/favorite', 'POST', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 638);

-- sys_menu_api: 挂到录像缓存页（140610）
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 638 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 638);

-- sys_casbin_rule: 管理员角色授权（policy 只增不删）
INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId/favorite', 'POST', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId/favorite' AND `v2` = 'POST');

-- ⛔ down 不可自动执行：删列会丢掉用户的收藏标记，删权限登记是 fail-closed 操作，需人工确认后再跑。
-- 如需回滚（仅开发库）：ALTER TABLE `gb_record_cache_task` DROP COLUMN `favorite`；
-- 再按 permission 删 sys_menu 140615 与 sys_role_menu、按 id 删 sys_api 638 与 sys_menu_api、
-- 按 (v1, v2) 删 sys_casbin_rule 中那条 role_1 策略。
