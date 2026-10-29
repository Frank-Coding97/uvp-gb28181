-- 与主迁移相同的 ANSI 语句；方言差异仅在大小写敏感的写法上。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'服务升级',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '设置定时检查更新' title,'/api/system/upgrade/notice/auto-check' path,'POST' method) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='system:upgrade:manage'
  AND a.path='/api/system/upgrade/notice/auto-check'
  AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE a.path='/api/system/upgrade/notice/auto-check'
  AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype='p' AND r.v0='role_1' AND r.v1=a.path AND r.v2=a.method);
