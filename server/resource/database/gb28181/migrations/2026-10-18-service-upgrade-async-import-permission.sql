INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询离线升级导入任务','/api/system/upgrade/import/:upload_id','GET','服务升级',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE BINARY path=BINARY '/api/system/upgrade/import/:upload_id' AND method='GET');
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='system:upgrade:read' AND a.path='/api/system/upgrade/import/:upload_id' AND a.method='GET'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1','/api/system/upgrade/import/:upload_id','GET','*','',''
WHERE NOT EXISTS (SELECT 1 FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/system/upgrade/import/:upload_id' AND v2='GET');
