-- 图像库按钮权限（菜单 140509 补权限码 + 两个按钮节点 + 接口重挂）
-- Migration: 2026-11-04-snapshot-library-permissions
--
-- 背景：图像库自 2026-09-20 上线时只有一条 type=2 页面菜单，permission 为空，
-- 页面可见性借用「多屏播放 → 设备图像抓拍会话」的 gb28181:device:snapshot，
-- 而「删除抓拍图像」(sys_api 639) 压根没挂到任何菜单 ⇒ 角色配置界面配不出删除权，
-- 只有系统管理员（casbin 里写死的 role_1）能用。本脚本把权限拆回图像库自己名下。
--
-- ⚠️ 本文件只用于把新物件补进**已有的开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 由
--    baseline/seeds 生成，已包含同样内容）。
--    与 2026-10-03-record-cache.sql 同一形态：仅 MySQL 方言、可重复执行。
--
-- ⛔ 页面菜单行也带 permission（`gb28181:snapshot:view`）：userservice 收集权限码时
--    取 type IN (2,3)，所以「勾了菜单 = 有查看权」，存量角色不必再补一行。
-- ⛔ 缩略图/取图走的 587 必须挂在这条 type=2 菜单上，**不能**归到下载按钮：
--    同一个接口既喂列表缩略图又喂单张下载，归到下载会让「只看不下载」的账号整页白图。

-- 1) 页面菜单补查看权限码（只在该列还不是目标值时改，保留原有 updated_at 语义之外的幂等性）
UPDATE `sys_menu`
SET `permission` = 'gb28181:snapshot:view', `updated_at` = NOW()
WHERE `id` = 140509 AND (`permission` IS NULL OR `permission` = '');

-- 2) 按钮节点：批量下载图像（对应 sys_api 686 /snapshots/archive）
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140620, 140509, '', '', NULL, '', '批量下载图像', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'gb28181:snapshot:download', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140620);

-- 3) 按钮节点：删除图像（对应 sys_api 639 DELETE /snapshots）
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 140621, 140509, '', '', NULL, '', '删除图像', 0, 0, 0, 1, 0, '', 0, '', '', 2, 3, 0, 'gb28181:snapshot:delete', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `id` = 140621);

-- 4) 接口重挂：图像库的读接口归到图像库菜单本身（原挂在 140450「设备图像抓拍会话」）
--    按 (path, method) 定位，避免写死 sys_api 自增 id 在不同环境里漂移。
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140509, a.`id` FROM `sys_api` a
WHERE (
        (a.`path` = '/api/gb28181/device-mgmt/snapshots' AND a.`method` = 'GET')
     OR (a.`path` = '/api/gb28181/device-mgmt/snapshots/:id/content' AND a.`method` = 'GET')
      )
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` ma WHERE ma.`menu_id` = 140509 AND ma.`api_id` = a.`id`);

-- 5) 接口重挂：批量下载 → 下载按钮
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140620, a.`id` FROM `sys_api` a
WHERE a.`path` = '/api/gb28181/device-mgmt/snapshots/archive' AND a.`method` = 'GET'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` ma WHERE ma.`menu_id` = 140620 AND ma.`api_id` = a.`id`);

-- 6) 接口重挂：删除 → 删除按钮
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140621, a.`id` FROM `sys_api` a
WHERE a.`path` = '/api/gb28181/device-mgmt/snapshots' AND a.`method` = 'DELETE'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` ma WHERE ma.`menu_id` = 140621 AND ma.`api_id` = a.`id`);

-- 7) 摘掉 140450 上的图像库接口（抓拍会话按钮只留 483/519 两条会话接口）
--    casbin 里已下发的策略不受影响：策略是授权时落库的，不随挂载关系回滚。
DELETE ma FROM `sys_menu_api` ma
JOIN `sys_api` a ON a.`id` = ma.`api_id`
WHERE ma.`menu_id` = 140450
  AND (
        (a.`path` = '/api/gb28181/device-mgmt/snapshots' AND a.`method` IN ('GET', 'DELETE'))
     OR (a.`path` = '/api/gb28181/device-mgmt/snapshots/:id/content' AND a.`method` = 'GET')
     OR (a.`path` = '/api/gb28181/device-mgmt/snapshots/archive' AND a.`method` = 'GET')
      );

-- 8) 存量角色补齐：**按 casbin 已有策略反查**，只给「本来就调得动这个接口」的角色补按钮。
--    ⛔ 不能写成「所有勾了图像库菜单的角色都补」—— 那等于凭空发删除权：
--       改造前 639 没挂任何菜单，除了 casbin 里显式写过 DELETE 的角色，谁也删不了。
--    这里以「接口权限 → 按钮」为准，保证按钮显示与后端放行口径一致。
INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT DISTINCT r.`id`, 140620 FROM `sys_casbin_rule` c
JOIN `sys_role` r ON c.`v0` = CONCAT('role_', r.`id`)
WHERE c.`v1` = '/api/gb28181/device-mgmt/snapshots/archive' AND c.`v2` = 'GET'
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` rm WHERE rm.`role_id` = r.`id` AND rm.`menu_id` = 140620);

INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT DISTINCT r.`id`, 140621 FROM `sys_casbin_rule` c
JOIN `sys_role` r ON c.`v0` = CONCAT('role_', r.`id`)
WHERE c.`v1` = '/api/gb28181/device-mgmt/snapshots' AND c.`v2` = 'DELETE'
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` rm WHERE rm.`role_id` = r.`id` AND rm.`menu_id` = 140621);
