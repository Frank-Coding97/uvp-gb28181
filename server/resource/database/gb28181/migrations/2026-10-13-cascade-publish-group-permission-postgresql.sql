INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,deleted_at,created_by)
SELECT '设置级联业务分组发布','/api/gb28181/cascade/platforms/:id/publish-group','PUT','国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/publish-group' AND method='PUT');
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.path='/api/gb28181/cascade/platforms/:id/publish-group' AND a.method='PUT'
WHERE m.permission='gb28181:cascade:manage'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1','/api/gb28181/cascade/platforms/:id/publish-group','PUT','*','',''
WHERE NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/cascade/platforms/:id/publish-group' AND v2='PUT');
