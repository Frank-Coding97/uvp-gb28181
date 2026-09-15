-- GB28181 cascade API/menu/Casbin seed (SQL Server 2017+, idempotent).
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,N'GB28181 国标级联',SYSUTCDATETIME(),SYSUTCDATETIME(),1
FROM (VALUES
 (N'查看级联平台列表',N'/api/gb28181/cascade/platforms',N'GET'),(N'创建级联平台',N'/api/gb28181/cascade/platforms',N'POST'),
 (N'查看级联平台',N'/api/gb28181/cascade/platforms/:id',N'GET'),(N'修改级联平台',N'/api/gb28181/cascade/platforms/:id',N'PUT'),(N'删除级联平台',N'/api/gb28181/cascade/platforms/:id',N'DELETE'),
 (N'启用级联平台',N'/api/gb28181/cascade/platforms/:id/enable',N'POST'),(N'停用级联平台',N'/api/gb28181/cascade/platforms/:id/disable',N'POST'),(N'更新级联启用状态',N'/api/gb28181/cascade/platforms/:id/enabled',N'PUT'),
 (N'重连级联平台',N'/api/gb28181/cascade/platforms/:id/reconnect',N'POST'),(N'查看级联共享',N'/api/gb28181/cascade/platforms/:id/shares',N'GET'),(N'更新级联共享',N'/api/gb28181/cascade/platforms/:id/shares',N'PUT')
) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
IF NOT EXISTS (SELECT 1 FROM sys_menu WHERE path=N'/gb28181/cascade' AND deleted_at IS NULL)
    INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
    VALUES (0,N'/gb28181/cascade',N'gb28181-cascade',N'gb28181/cascade/index',N'国标级联',0,0,13,2,N'',N'lucide:GitBranch',SYSUTCDATETIME(),SYSUTCDATETIME(),1);
DECLARE @GB_CASCADE_MENU_ID INT;
SELECT TOP 1 @GB_CASCADE_MENU_ID=id FROM sys_menu WHERE path=N'/gb28181/cascade' AND deleted_at IS NULL ORDER BY id;
UPDATE sys_menu SET parent_id=@GB_CASCADE_MENU_ID, updated_at=SYSUTCDATETIME()
WHERE permission IN (N'gb28181:cascade:view',N'gb28181:cascade:manage',N'gb28181:cascade:enable',N'gb28181:cascade:share',N'gb28181:cascade:reconnect') AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by)
SELECT m.id,N'',N'gb28181-cascade-'+v.permission,N'',N'国标级联',1,3,N'gb28181:cascade:'+v.permission,SYSUTCDATETIME(),SYSUTCDATETIME(),1
FROM sys_menu m CROSS JOIN (VALUES (N'view'),(N'manage'),(N'enable'),(N'share'),(N'reconnect')) v(permission)
WHERE m.path=N'/gb28181/cascade' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.permission=N'gb28181:cascade:'+v.permission AND x.deleted_at IS NULL);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:cascade:view' AND a.path LIKE N'/api/gb28181/cascade/%' AND a.method=N'GET' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:cascade:manage' AND a.path IN (N'/api/gb28181/cascade/platforms',N'/api/gb28181/cascade/platforms/:id') AND a.method IN (N'POST',N'PUT',N'DELETE') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE ((m.permission=N'gb28181:cascade:enable' AND a.path LIKE N'/api/gb28181/cascade/platforms/%/enab%') OR (m.permission=N'gb28181:cascade:share' AND a.path=N'/api/gb28181/cascade/platforms/:id/shares') OR (m.permission=N'gb28181:cascade:reconnect' AND a.path=N'/api/gb28181/cascade/platforms/:id/reconnect'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.permission IN (N'gb28181:cascade:view',N'gb28181:cascade:manage',N'gb28181:cascade:enable',N'gb28181:cascade:share',N'gb28181:cascade:reconnect') AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.path=N'/gb28181/cascade' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT N'p',N'role_1',a.path,a.method,N'*',N'',N'' FROM sys_api a WHERE a.path LIKE N'/api/gb28181/cascade/%' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype=N'p' AND c.v0=N'role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3=N'*');
