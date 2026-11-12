-- 服务升级「定时检查更新」开关接口的权限登记（幂等）。
-- 开关只影响助手进程的定时探测，属于管理动作。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'服务升级',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '设置定时检查更新' title,'/api/system/upgrade/notice/auto-check' path,'POST' method) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:upgrade:manage'
  AND BINARY a.path=BINARY '/api/system/upgrade/notice/auto-check'
  AND BINARY a.method=BINARY 'POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE BINARY a.path=BINARY '/api/system/upgrade/notice/auto-check'
  AND BINARY a.method=BINARY 'POST'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY 'role_1' AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);
