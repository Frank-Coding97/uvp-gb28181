-- 服务升级「新版本提示」三个接口的权限登记（幂等）。
-- 徽标读取的是助手缓存，GET 归查看权限；忽略/取消忽略是管理动作。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'服务升级',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查看更新提示' title,'/api/system/upgrade/notice' path,'GET' method
      UNION ALL SELECT '忽略更新版本','/api/system/upgrade/notice/ignore','POST'
      UNION ALL SELECT '取消忽略更新版本','/api/system/upgrade/notice/ignore','DELETE') v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:upgrade:read'
  AND BINARY a.path=BINARY '/api/system/upgrade/notice'
  AND BINARY a.method=BINARY 'GET'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:upgrade:manage'
  AND BINARY a.path=BINARY '/api/system/upgrade/notice/ignore'
  AND BINARY a.method IN (BINARY 'POST',BINARY 'DELETE')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE BINARY a.path IN (BINARY '/api/system/upgrade/notice',BINARY '/api/system/upgrade/notice/ignore')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY 'role_1' AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);
