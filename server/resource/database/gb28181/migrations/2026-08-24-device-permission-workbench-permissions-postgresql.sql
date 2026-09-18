-- 设备权限工作台 API 权限迁移(PostgreSQL,幂等)。

UPDATE sys_menu SET title='设备权限工作台', updated_at=CURRENT_TIMESTAMP
WHERE name='device-assignment' AND deleted_at IS NULL;

UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND path IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM sys_menu_api ma USING sys_api a
WHERE a.id=ma.api_id AND a.path IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM sys_casbin_rule WHERE v1 IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');

UPDATE sys_api SET deleted_at=NULL, title='查询设备权限汇总', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/summary' AND method='GET';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询设备权限汇总','/api/gb28181/device-mgmt/permission-workbench/summary','GET','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/summary' AND method='GET' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='解析工作台设备', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '解析工作台设备','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='查询设备共享授权', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/query','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='查询共享目标', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询共享目标','/api/gb28181/device-mgmt/permission-workbench/grant-targets','GET','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='调整设备归属', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='整部门调整设备归属', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '整部门调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='应用设备共享授权', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '应用设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/apply','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.name='device-assignment' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND ((a.path='/api/gb28181/device-mgmt/permission-workbench/summary' AND a.method='GET') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.method='POST') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.method='POST') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.permission='gb28181:device:assign' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path IN ('/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments') AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.permission='gb28181:device:share' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.name IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND a.path LIKE '/api/gb28181/device-mgmt/permission-workbench/%'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
