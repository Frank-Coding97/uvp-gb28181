-- 固件仓库菜单补齐（幂等 backfill）。
--
-- 背景：`/gb28181/firmware-repo/index` 这条顶级菜单由 2026-10-02 的迁移引入，
-- 但那次迁移未进入 migrationsfs 的 embed 白名单（白名单自 2026-10-08 起）。
-- 结果：全新安装（走 baseline.sql）有该菜单；**存量库升级后没有**，
-- 前端路由由后端菜单驱动（route-output.ts），菜单缺失 ⇒ 路由不注册 ⇒ 直达 URL 404。
--
-- 本迁移只做「缺则补」，不覆盖已有配置：已存在同名 path / permission 时整段跳过。
-- 定位键一律用 path / permission，不写死自增 id。

-- 1) 顶级菜单：与「设备列表」平级，排在 OpenAPI 客户端之后（sort=150）
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,disable,icon,sort,created_at,updated_at,created_by)
SELECT 0,'/gb28181/firmware-repo/index','firmware-repository-list','gb28181/firmware-repo/index',N'固件仓库',2,'',0,0,'lucide:Box',150,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT 1 AS _firmware_backfill_dummy) d
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/firmware-repo/indexN' AND deleted_at IS NULL);

-- 2) 页面按钮权限：上传 / 删除 / 下载
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,sort,created_at,updated_at,created_by)
SELECT m.id,'','','',N'上传固件',3,'gb28181:firmware-repository:upload',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/gb28181/firmware-repo/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:firmware-repository:upload' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,sort,created_at,updated_at,created_by)
SELECT m.id,'','','',N'删除固件',3,'gb28181:firmware-repository:delete',2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/gb28181/firmware-repo/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:firmware-repository:delete' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,sort,created_at,updated_at,created_by)
SELECT m.id,'','','',N'下载固件',3,'gb28181:firmware-repository:download',3,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/gb28181/firmware-repo/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:firmware-repository:downloadN' AND deleted_at IS NULL);

-- 3) 设备维护入口的按钮权限（挂在「设备列表」下，与基线一致）
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,sort,created_at,updated_at,created_by)
SELECT m.id,'','GbFirmwareRepositoryManage','',N'管理固件仓库',3,'gb28181:firmware-repository:manage',2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/gb28181/device-mgmt/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:firmware-repository:manageN' AND deleted_at IS NULL);

-- 4) 给启用的系统管理员角色补授权（只补不删）
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT r.id,m.id FROM sys_role r CROSS JOIN sys_menu m
WHERE r.id=1 AND r.status=1 AND r.deleted_at IS NULL AND m.deleted_at IS NULL
AND (m.path='/gb28181/firmware-repo/index'
     OR m.permission IN ('gb28181:firmware-repository:upload','gb28181:firmware-repository:delete','gb28181:firmware-repository:download','gb28181:firmware-repository:manage'))
AND NOT EXISTS (SELECT 1 FROM sys_role_menu rm WHERE rm.role_id=r.id AND rm.menu_id=m.id);
