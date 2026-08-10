-- GB28181 cascade API/menu/Casbin seed (PostgreSQL 12+, idempotent).
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 ('查看级联平台列表','/api/gb28181/cascade/platforms','GET'),('创建级联平台','/api/gb28181/cascade/platforms','POST'),
 ('查看级联平台','/api/gb28181/cascade/platforms/:id','GET'),('修改级联平台','/api/gb28181/cascade/platforms/:id','PUT'),('删除级联平台','/api/gb28181/cascade/platforms/:id','DELETE'),
 ('启用级联平台','/api/gb28181/cascade/platforms/:id/enable','POST'),('停用级联平台','/api/gb28181/cascade/platforms/:id/disable','POST'),('更新级联启用状态','/api/gb28181/cascade/platforms/:id/enabled','PUT'),
 ('重连级联平台','/api/gb28181/cascade/platforms/:id/reconnect','POST'),('查看级联共享','/api/gb28181/cascade/platforms/:id/shares','GET'),('更新级联共享','/api/gb28181/cascade/platforms/:id/shares','PUT')
) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','gb28181-cascade-'||v.permission,'','国标级联',TRUE,3,'gb28181:cascade:'||v.permission,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m CROSS JOIN (VALUES ('view'),('manage'),('enable'),('share'),('reconnect')) v(permission)
WHERE m.path='/gb28181' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.permission='gb28181:cascade:'||v.permission AND x.deleted_at IS NULL);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:cascade:view' AND a.path LIKE '/api/gb28181/cascade/%' AND a.method='GET' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:cascade:manage' AND a.path IN ('/api/gb28181/cascade/platforms','/api/gb28181/cascade/platforms/:id') AND a.method IN ('POST','PUT','DELETE') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:cascade:enable' AND a.path LIKE '/api/gb28181/cascade/platforms/%/enab%') OR (m.permission='gb28181:cascade:share' AND a.path='/api/gb28181/cascade/platforms/:id/shares') OR (m.permission='gb28181:cascade:reconnect' AND a.path='/api/gb28181/cascade/platforms/:id/reconnect'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.permission IN ('gb28181:cascade:view','gb28181:cascade:manage','gb28181:cascade:enable','gb28181:cascade:share','gb28181:cascade:reconnect') AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path LIKE '/api/gb28181/cascade/%' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
