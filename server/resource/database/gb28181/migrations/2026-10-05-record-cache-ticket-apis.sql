-- 录像缓存「票据下载」三条**鉴权**路由的接口登记与授权
-- Migration: 2026-10-05-record-cache-ticket-apis
--
-- 背景：「方案①」把下载改成浏览器原生下载 —— 带 JWT 的入口只负责**签发**一张一次性票据，
-- 真正下文件的路由挂在免鉴权裸 engine 上（浏览器自己发起的下载带不了 JWT 头）。
-- 本次新增 3 条**鉴权**路由（挂在 gb.Group("/record-cache") 下），
-- 而 2026-10-03-record-cache.sql 只登记了 629-634（老的六个接口）：
--
--   POST   /api/gb28181/record-cache/tasks/:taskId/downloads   签发下载票据
--   GET    /api/gb28181/record-cache/downloads/:downloadId     查询下载状态
--   DELETE /api/gb28181/record-cache/downloads/:downloadId     取消下载
--
-- ⛔ 另外两条**免鉴权**路由**故意不登记**（与同仓 cloud-recordings 保持同一惯例）：
--   GET /api/gb28181/record-cache/downloads/:downloadId/content
--   GET /api/gb28181/record-cache/downloads/:downloadId/segments/:index/content
--   它们的鉴权凭证是 HttpOnly cookie 里的一次性票据（60 秒有效、单次消费、
--   cookie Path 锁定到那一段），根本不经过 JWT / casbin。登记进 sys_api 只会造成
--   「登记了却不受控」的假象。⇒ cloud-recordings 的两条同类路由同样从未登记
--   （sys_api 里搜不到 cloud-recordings 的 /content）。
--
-- ⚠️ 与 2026-10-03-record-cache.sql 同形态：仅 MySQL 方言、可重复执行，
--    用于把新物件补进**已有开发库**；全新环境由三方言全量脚本建库
--    （uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql 已包含同样内容）。

-- sys_api: 票据下载接口（3 个）
INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 635, '创建录像缓存下载', '/api/gb28181/record-cache/tasks/:taskId/downloads', 'POST', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 635);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 636, '查询录像缓存下载', '/api/gb28181/record-cache/downloads/:downloadId', 'GET', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 636);

INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `deleted_at`, `created_by`)
SELECT 637, '取消录像缓存下载', '/api/gb28181/record-cache/downloads/:downloadId', 'DELETE', '录像缓存', NOW(), NOW(), NULL, 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `id` = 637);

-- sys_menu_api: 挂到录像缓存页（140610）。
-- ⛔ 不挂 140442（回放页的「设备录像下载」权限点）：回放页的进度弹窗只有进度与取消，
-- 没有下载入口，下载动作只发生在录像缓存页。
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 635 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 635);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 636 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 636);
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT 140610, 637 WHERE NOT EXISTS (SELECT 1 FROM `sys_menu_api` WHERE `menu_id` = 140610 AND `api_id` = 637);

-- sys_casbin_rule: 管理员角色授权（policy 只增不删）
INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/tasks/:taskId/downloads', 'POST', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/tasks/:taskId/downloads' AND `v2` = 'POST');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/downloads/:downloadId', 'GET', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/downloads/:downloadId' AND `v2` = 'GET');

INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`)
SELECT 'p', 'role_1', '/api/gb28181/record-cache/downloads/:downloadId', 'DELETE', '*', '', ''
WHERE NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` WHERE `ptype` = 'p' AND `v0` = 'role_1' AND `v1` = '/api/gb28181/record-cache/downloads/:downloadId' AND `v2` = 'DELETE');

-- ⛔ down 不可自动执行：删除接口登记与授权是 fail-closed 操作，需人工确认后再跑。
-- 如需回滚（仅开发库）：按 id 删 sys_api 635/636/637、按 (menu_id, api_id) 删 sys_menu_api、
-- 按 (v1, v2) 删 sys_casbin_rule 中这三条 role_1 策略。
