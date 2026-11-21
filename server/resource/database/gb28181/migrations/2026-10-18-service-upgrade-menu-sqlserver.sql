-- 服务升级纳入系统管理；保留已有菜单 ID，只向启用的系统管理员补齐授权。
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,disable,icon,sort,created_at,updated_at,created_by)
SELECT p.id,'/system/service-upgrade/index','service-upgrade','system/service-upgrade/index',N'服务升级',2,'',0,0,'lucide:CloudDownload',90,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu p WHERE p.path='/system' AND p.type=1 AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/system/service-upgrade/index' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type=1 AND deleted_at IS NULL) service_upgrade_parent),hide=0,disable=0,icon='lucide:CloudDownload',sort=90
WHERE path='/system/service-upgrade/index' AND deleted_at IS NULL
AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type=1 AND deleted_at IS NULL) service_upgrade_parent) IS NOT NULL;
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','ServiceUpgradeRead','',N'查看服务升级',3,'system:upgrade:read',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/system/service-upgrade/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:upgrade:read' AND deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','ServiceUpgradeManage','',N'管理服务升级',3,'system:upgrade:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m WHERE m.path='/system/service-upgrade/index' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:upgrade:manage' AND deleted_at IS NULL);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT r.id,m.id FROM sys_role r CROSS JOIN sys_menu m
WHERE r.id=1 AND r.status=1 AND r.deleted_at IS NULL AND m.deleted_at IS NULL
AND (m.path IN ('/system','/system/service-upgrade/index') OR m.permission IN ('system:upgrade:read','system:upgrade:manage'))
AND NOT EXISTS (SELECT 1 FROM sys_role_menu rm WHERE rm.role_id=r.id AND rm.menu_id=m.id);
