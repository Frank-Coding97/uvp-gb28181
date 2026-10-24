-- 在线升级菜单、精细权限及 API 注册。
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT 0,'/system/service-upgrade/index','service-upgrade','system/service-upgrade/index','服务升级',2,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE BINARY path=BINARY '/system/service-upgrade/index' AND BINARY name=BINARY 'service-upgrade');
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','ServiceUpgradeRead','', '查看服务升级',3,'system:upgrade:read',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m
WHERE BINARY m.path=BINARY '/system/service-upgrade/index'
  AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE BINARY permission=BINARY 'system:upgrade:read');
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','ServiceUpgradeManage','', '管理服务升级',3,'system:upgrade:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m
WHERE BINARY m.path=BINARY '/system/service-upgrade/index'
  AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE BINARY permission=BINARY 'system:upgrade:manage');

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'服务升级',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查看升级状态' title,'/api/system/upgrade/status' path,'GET' method
      UNION ALL SELECT '查看无人值守策略','/api/system/upgrade/policy','GET'
      UNION ALL SELECT '查看迁移计划','/api/system/upgrade/migration-plan','GET'
      UNION ALL SELECT '检查在线升级','/api/system/upgrade/check','POST'
      UNION ALL SELECT '查看升级任务','/api/system/upgrade/tasks/:id','GET'
      UNION ALL SELECT '导入离线升级包','/api/system/upgrade/import','POST'
      UNION ALL SELECT '创建升级任务','/api/system/upgrade/tasks','POST'
      UNION ALL SELECT '修改无人值守策略','/api/system/upgrade/policy','PUT'
      UNION ALL SELECT '取消升级任务','/api/system/upgrade/tasks/:id/cancel','POST'
      UNION ALL SELECT '应用升级任务','/api/system/upgrade/tasks/:id/apply','POST'
      UNION ALL SELECT '回退升级任务','/api/system/upgrade/tasks/:id/rollback','POST') v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:upgrade:read'
  AND BINARY a.method IN (BINARY 'GET',BINARY 'POST')
  AND BINARY a.path IN (BINARY '/api/system/upgrade/status',BINARY '/api/system/upgrade/policy',BINARY '/api/system/upgrade/migration-plan',BINARY '/api/system/upgrade/check',BINARY '/api/system/upgrade/tasks/:id')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:upgrade:manage'
  AND BINARY a.method IN (BINARY 'POST',BINARY 'PUT')
  AND BINARY a.path IN (BINARY '/api/system/upgrade/policy',BINARY '/api/system/upgrade/import',BINARY '/api/system/upgrade/tasks',BINARY '/api/system/upgrade/tasks/:id/cancel',BINARY '/api/system/upgrade/tasks/:id/apply',BINARY '/api/system/upgrade/tasks/:id/rollback')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE BINARY a.path IN (BINARY '/api/system/upgrade/status',BINARY '/api/system/upgrade/policy',BINARY '/api/system/upgrade/migration-plan',BINARY '/api/system/upgrade/check',BINARY '/api/system/upgrade/tasks/:id',BINARY '/api/system/upgrade/import',BINARY '/api/system/upgrade/tasks',BINARY '/api/system/upgrade/tasks/:id/cancel',BINARY '/api/system/upgrade/tasks/:id/apply',BINARY '/api/system/upgrade/tasks/:id/rollback')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY 'role_1' AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);
