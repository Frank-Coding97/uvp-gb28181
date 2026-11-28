-- 定时任务日志「一键清理」接口的权限数据（一次性数据修正，幂等可重跑）
--
-- 背景：定时任务日志页（菜单 140347）新增「一键清理」按钮，后端新增路由
--   POST /api/sysJobResults/clear（server/app/routes/routes.go）
-- 三方对账后需要补三处数据 + 显式物化 casbin：
--   A. sys_api        登记该接口（不登记 ⇒ 永远无法被授权，非 admin 必 403）
--   B. sys_menu       新增 type=3 权限点 system:sysjobresults:clear（按钮可见性）
--   C. sys_menu_api   把权限点挂到接口上（后端闸门）
--   D. sys_casbin_rule 物化 role_1 的策略（casbin 不会随 sys_menu_api 自动重算）
--
-- 生效方式：casbin.autoloadpolicyseconds=120 ⇒ 落库后 ≤2 分钟自动重读；
--   uid=1 的 admin 在 server.notcheckuser 里，本就绕过 casbin（所以 admin 测不出来）。
-- 幂等性：INSERT IGNORE 依赖主键去重，重复执行不报错；down 只删本脚本造的行。
--   ⚠️ sys_casbin_rule.v4/v5 现有行均为空串（列默认 NULL），必须显式写 ''，
--      否则唯一键 (ptype,v0,v1,v2,v3,v4,v5) 对不上，去重失效。

-- A. 接口登记
INSERT IGNORE INTO sys_api (id, title, path, method, api_group, created_at, updated_at, deleted_at, created_by) VALUES
(687, '清空定时任务日志', '/api/sysJobResults/clear', 'POST', '日志中心', '2026-11-04 18:55:00.000000', '2026-11-04 18:55:00.000000', NULL, 1);

-- B. 权限点（挂在定时任务日志页 140347 下，与既有 140348「删除」同级）
INSERT IGNORE INTO sys_menu (id, parent_id, path, name, redirect, component, title, is_full, hide, disable, keep_alive, affix, link, iframe, svg_icon, icon, sort, type, is_link, permission, created_at, updated_at, deleted_at, created_by) VALUES
(140619, 140347, '', 'SysJobResultsClear', NULL, '', '一键清理', 0, 0, 0, 0, 0, '', 0, '', '', 1, 3, 0, 'system:sysjobresults:clear', '2026-11-04 18:55:00.000000', '2026-11-04 18:55:00.000000', NULL, 1);

-- C. 挂载
INSERT IGNORE INTO sys_menu_api (menu_id, api_id) VALUES (140619, 687);

-- C2. 角色授权：前端按钮可见性看的是 sys_role_menu × sys_menu.permission，
--     只加菜单不授权 ⇒ 连 admin 也看不到按钮（role_1 = 系统管理员）。
INSERT IGNORE INTO sys_role_menu (role_id, menu_id) VALUES (1, 140619);

-- D. casbin 物化（只补不删）
INSERT IGNORE INTO sys_casbin_rule (id, ptype, v0, v1, v2, v3, v4, v5) VALUES
(10359, 'p', 'role_1', '/api/sysJobResults/clear', 'POST', '*', '', '');
