-- 设备权限工作台 API 权限回滚(PostgreSQL,可重复执行)。
DELETE FROM sys_casbin_rule WHERE v1 IN ('/api/gb28181/device-mgmt/permission-workbench/summary','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','/api/gb28181/device-mgmt/permission-workbench/grants/query','/api/gb28181/device-mgmt/permission-workbench/grant-targets','/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','/api/gb28181/device-mgmt/permission-workbench/grants/apply');
DELETE FROM sys_menu_api ma USING sys_menu m, sys_api a
WHERE m.id=ma.menu_id AND a.id=ma.api_id
  AND m.name IN ('device-assignment','device-assignment-assign','device-assignment-share')
  AND a.path IN ('/api/gb28181/device-mgmt/permission-workbench/summary','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','/api/gb28181/device-mgmt/permission-workbench/grants/query','/api/gb28181/device-mgmt/permission-workbench/grant-targets','/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND path IN ('/api/gb28181/device-mgmt/permission-workbench/summary','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','/api/gb28181/device-mgmt/permission-workbench/grants/query','/api/gb28181/device-mgmt/permission-workbench/grant-targets','/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE sys_menu SET title='设备分配', updated_at=CURRENT_TIMESTAMP
WHERE name='device-assignment' AND title='设备权限工作台' AND deleted_at IS NULL;
