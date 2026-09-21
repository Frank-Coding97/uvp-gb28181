-- 存储卡格式化（GB/T 28181-2022 A.2.3.1.13）的权限
-- —— PostgreSQL 方言，与 2026-09-20-storage-card-format-permission.sql 等价。
--
-- ⛔ 为什么必须独立路由 / 独立权限码 `gb28181:device:format_sd`：见 MySQL 版本注释
--    （核心是 `/channel/:id/device-control` 整条绑 `gb28181:device:control`，
--     做成 action 就等于没有独立授权）。同族先例是远程重启。
--
-- 幂等：五处（sys_api / sys_menu / sys_role_menu / sys_menu_api / sys_casbin_rule）
--       全部 NOT EXISTS 守卫。Casbin 段用 `'role_' || rm.role_id`，NOT EXISTS 里写字面量。

-- storage-card-format-permissions:start

-- ---- 接口：POST /api/gb28181/device-mgmt/channel/:id/storage-cards/format ----
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '格式化存储卡','/api/gb28181/device-mgmt/channel/:id/storage-cards/format','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST' AND deleted_at IS NULL);

-- ---- 按钮权限点：gb28181:device:format_sd ----
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceStorageCardFormat','','格式化存储卡',1,0,1,3,'gb28181:device:format_sd','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:format_sd' AND deleted_at IS NULL);

-- ⛔ 授权必须显式写 sys_role_menu：菜单树是 role_menu 驱动，少了它权限点对谁都不生效。
-- ⛔ JOIN 条件必须带 `type=3`（同 permission 的目录行不是按钮，见 MySQL 版本注释）。
-- 只给 role 1（内置系统管理员），其余角色由运营在「菜单管理」按需授权。
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
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:format_sd' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND p.v2='POST' AND p.v3='*');

-- storage-card-format-permissions:end
