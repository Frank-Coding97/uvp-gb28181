-- 异步视频探针查询 API，复用 gb28181:play:diagnose 权限。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询视频探针任务','/api/gb28181/stream-probes/operations/:operationId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/stream-probes/operations/:operationId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:play:diagnose' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/stream-probes/operations/:operationId' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:play:diagnose' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/stream-probes/operations/:operationId' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
