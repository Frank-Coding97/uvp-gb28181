-- 存储卡格式化（GB/T 28181-2022 A.2.3.1.13「存储卡格式化控制命令」）的权限。
--
-- 一个**破坏性**动作 + 一个**独立**权限码 `gb28181:device:format_sd`：
-- 下发后卡上的录像会被清空，所以不能跟"按录像 / 布撤防"这类可逆控制同级。
--
-- ⛔ 为什么必须是**独立路由**（`POST /channel/:id/storage-cards/format`），
--    而不是把 `format_sd` 做成 `/channel/:id/device-control` 的一个 action：
--    那条路由整条绑在 `gb28181:device:control` 上（见 2026-09-05-button-permission-catalog.sql），
--    塞进去的话本权限码在 Casbin 层与普通设备控制**完全同权** ——
--    账号只要能动录像，就能格式化存储卡，"单独授权"这件事根本没发生。
--    同族先例是远程重启（独立路由 `/device/:id/reboot` + `gb28181:device:reboot`）。
--
-- ⛔ 路由全路径 `/api/gb28181/device-mgmt/channel/:id/storage-cards/format` 三处必须同名
--    （迁移 / 路由 / 测试）。路由侧从 `models.StorageCardFormatRoutePath` 派生，
--    手写错一个字符的表现是"接口通但恒 403"，且两侧都不报错。
--
-- 幂等：sys_api / sys_menu / sys_role_menu / sys_menu_api / sys_casbin_rule 五处全部
--       NOT EXISTS 守卫，连跑两遍无副作用。
--
-- ⛔ Casbin 段的 NOT EXISTS 里**不许写列列比较**（`p.v1=a.path` 这类）：
--    `sys_casbin_rule.v*` 是 utf8mb4_0900_ai_ci、`sys_api.path` 是 utf8mb4_unicode_ci，
--    MySQL 会报 1267 Illegal mix of collations → runner 遇错中止且不 MarkApplied →
--    后端起不来（2026-09-19 实测）。外层 WHERE 已把 path/method 钉成常量，
--    所以这里直接写字面量；`p.v0=CONCAT('role_',rm.role_id)` 是安全的。

-- storage-card-format-permissions:start

-- ---- 接口：POST /api/gb28181/device-mgmt/channel/:id/storage-cards/format ----
-- 请求体 {"cardIndex":<int>,"confirmed":true,"idempotencyKey":"…"}；
-- cardIndex 语义来自标准注释「SD 卡编号，从1开始编号。该值0时，对所有存储卡进行格式化」。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '格式化存储卡','/api/gb28181/device-mgmt/channel/:id/storage-cards/format','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST' AND deleted_at IS NULL);

-- ---- 按钮权限点：gb28181:device:format_sd ----
-- hide=1 / type=3：按钮点不出现在菜单树里，只用于权限判定与"菜单管理"里的授权项
-- （前端要能发现它，否则运营不知道该给谁授权）。parent 挂设备管理列表，与重启按钮同族。
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceStorageCardFormat','','格式化存储卡',1,0,1,3,'gb28181:device:format_sd','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:format_sd' AND deleted_at IS NULL);

-- ⛔ 授权必须显式写 sys_role_menu：本平台菜单树是 role_menu 驱动
-- （app/service/userservice.go 按用户的 role_id 去 sys_role_menu 取 menu_id），
-- 少了这一行，权限点存在、任何人都不生效（含内置管理员）。
-- ⛔ JOIN 条件必须带 `type=3`：同 permission 的**目录行（type=1）不是按钮**，
--    只按 permission 授权会把一个隐藏导航壳也授给 role 1（凭空多一个菜单入口）。
--    下面 sys_menu_api / sys_casbin_rule 两段同样带 type=3 —— 三处口径必须一致。
-- 只给 role 1（内置系统管理员）；其他角色由运营在「菜单管理」按需授权 ——
-- 破坏性动作的默认面越小越好。
INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission='gb28181:device:format_sd' AND m.type=3 AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:format_sd' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:format_sd' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND p.v2='POST' AND p.v3='*');

-- storage-card-format-permissions:end
