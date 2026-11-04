-- 图像库「批量下载（打包 zip）」接口的登记与授权
-- Migration: 2026-11-04-snapshot-archive-download
--
-- 背景：图像库的批量下载原先是前端逐张触发 `<a download>` —— 浏览器会把连续多次下载
-- 判成弹窗批量拦截，表现是"提示下了 20 张、下载目录里只有 8 个"。改成由后端打成一个 zip
-- 流式返回，于是新增一条**鉴权**路由（挂在 gb.Group("/device-mgmt") 下）：
--
--   GET /api/gb28181/device-mgmt/snapshots/archive?ids=1,2,3[&preflight=1]
--
-- 这条路径与 `/snapshots/:id/content` 在同一层混排了静态段 `archive` 与参数段 `:id`：
-- gin v1.11 允许（注册不 panic），且两段深度不同，不会互相吃掉。
--
-- 权限面与读图/列表接口一致（都是 `gb28181:device:snapshot` 那个抓拍按钮 140450）：
-- 能看这批图的人才能把这批图下载走，否则等于绕过"只读账号不许下载"的口径。
--
-- ⚠️ 与 2026-10-05-record-cache-ticket-apis.sql 同形态：仅 MySQL 方言、可重复执行，
--    用于把新物件补进**已有开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 已包含同样内容）。

-- sys_api: 批量下载接口（1 个）
INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 686, '批量下载抓拍图像', '/api/gb28181/device-mgmt/snapshots/archive', 'GET', '图像库', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 686);

-- sys_menu_api: 挂到「设备图像抓拍会话」按钮（140450）—— 与读图(587)/列表(589)同一权限面。
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140450, 686 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140450 AND `api_id` = 686);

-- sys_casbin_rule: 管理员角色授权（policy 只增不删）
INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/device-mgmt/snapshots/archive', 'GET', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule`
    WHERE `ptype` = 'p' AND `v0` = 'role_1'
      AND `v1` = '/api/gb28181/device-mgmt/snapshots/archive' AND `v2` = 'GET');

-- ⛔ down 不可自动执行：删除接口登记与授权是 fail-closed 操作，需人工确认后再跑。
-- 如需回滚（仅开发库）：按 id 删 sys_api 686、按 (menu_id, api_id) 删 sys_menu_api (140450, 686)、
-- 按 (v1, v2) 删 sys_casbin_rule 中该条 role_1 策略。
