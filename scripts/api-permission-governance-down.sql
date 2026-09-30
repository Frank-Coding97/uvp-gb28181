-- 回滚: 撤销 api-permission-governance.sql 的全部改动
-- 生成源: tmp/perm_governance/gen_delivery.py —— 幂等可重跑
-- 原值来源: 改动前 220 开发库快照（sys_api 407 行 / sys_menu_api 428 行 / sys_casbin_rule 684 行）
-- 执行顺序不可调换：casbin 与挂载的删除必须发生在登记/挂载被移除之前。

-- D'. 撤销本轮新增的 casbin 策略（按新增挂载推导，须在 C'/B' 之前执行）
--     已核实：改动前这些 (role,path,method) 在 casbin 中零存在 ⇒ 删除即精确回退
DELETE c FROM sys_casbin_rule c
JOIN sys_role_menu rm ON c.v0 = CONCAT('role_', rm.role_id) COLLATE utf8mb4_unicode_ci
JOIN sys_menu_api ma ON ma.menu_id = rm.menu_id
JOIN sys_api a ON a.id = ma.api_id
WHERE c.ptype = 'p'
  AND (ma.menu_id, ma.api_id) IN (
(1005, 28),
(1005, 42),
(140235, 46),
(140235, 47),
(140239, 59),
(140265, 106),
(140265, 187),
(140371, 277),
(140371, 278),
(140371, 279),
(140371, 281),
(140371, 283),
(140371, 284),
(140371, 292),
(140452, 597),
(140452, 598),
(140452, 599),
(140362, 600),
(140362, 601),
(140369, 602),
(140369, 603),
(140225, 604),
(140377, 605),
(140459, 606),
(140376, 607),
(140452, 608),
(140452, 609),
(140452, 611),
(140366, 610),
(140359, 612),
(140483, 613),
(140379, 617),
(140379, 618),
(140475, 619))
  AND c.v1 = a.path   COLLATE utf8mb4_unicode_ci
  AND c.v2 = a.method COLLATE utf8mb4_unicode_ci;

-- C'. 撤销新增挂载
DELETE FROM sys_menu_api WHERE (menu_id, api_id) IN (
(1005, 28),
(1005, 42),
(140235, 46),
(140235, 47),
(140239, 59),
(140265, 106),
(140265, 187),
(140371, 277),
(140371, 278),
(140371, 279),
(140371, 281),
(140371, 283),
(140371, 284),
(140371, 292),
(140452, 597),
(140452, 598),
(140452, 599),
(140362, 600),
(140362, 601),
(140369, 602),
(140369, 603),
(140225, 604),
(140377, 605),
(140459, 606),
(140376, 607),
(140452, 608),
(140452, 609),
(140452, 611),
(140366, 610),
(140359, 612),
(140483, 613),
(140379, 617),
(140379, 618),
(140475, 619));

-- B'. 撤销新增登记（先清其挂载，避免悬空）
DELETE FROM sys_menu_api WHERE api_id BETWEEN 597 AND 619;
DELETE FROM sys_api WHERE id BETWEEN 597 AND 619;

-- A'. 还原被删的幽灵登记（含原 created_at/updated_at）
INSERT IGNORE INTO sys_api (id, title, path, method, api_group, created_at, updated_at, deleted_at, created_by) VALUES
(3, '生成验证码ID', '/api/captcha/id', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(4, '获取验证码图片', '/api/captcha/image', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1),
(64, '查看内存缓存', '/api/config/viewCache', 'GET', '系统配置', '2025-10-10 17:41:33', '2025-10-10 17:41:33', NULL, 1),
(202, '切换租户', '/api/users/switchTenant/:tenantld', 'GET', '用户管理', '2026-01-09 16:29:37', '2026-01-09 16:29:37', NULL, 1);

-- A'. 还原被连带删除的挂载
INSERT IGNORE INTO sys_menu_api (menu_id, api_id) VALUES
(10, 202),
(140245, 64);
