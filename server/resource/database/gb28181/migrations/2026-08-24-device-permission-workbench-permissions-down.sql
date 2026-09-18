-- 设备权限工作台 API 权限回滚(MySQL,可重复执行)。
-- 只移除本迁移新增的关系；旧 API 仍保持软删除，不在 down 中复活。
DELETE FROM `sys_casbin_rule`
WHERE `v1` IN ('/api/gb28181/device-mgmt/permission-workbench/summary',
               '/api/gb28181/device-mgmt/permission-workbench/devices/resolve',
               '/api/gb28181/device-mgmt/permission-workbench/grants/query',
               '/api/gb28181/device-mgmt/permission-workbench/grant-targets',
               '/api/gb28181/device-mgmt/permission-workbench/assignments',
               '/api/gb28181/device-mgmt/permission-workbench/assignments/departments',
               '/api/gb28181/device-mgmt/permission-workbench/grants/apply');
DELETE ma FROM `sys_menu_api` ma
JOIN `sys_menu` m ON m.`id`=ma.`menu_id`
JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE m.`name` IN ('device-assignment','device-assignment-assign','device-assignment-share')
  AND a.`path` IN ('/api/gb28181/device-mgmt/permission-workbench/summary',
                   '/api/gb28181/device-mgmt/permission-workbench/devices/resolve',
                   '/api/gb28181/device-mgmt/permission-workbench/grants/query',
                   '/api/gb28181/device-mgmt/permission-workbench/grant-targets',
                   '/api/gb28181/device-mgmt/permission-workbench/assignments',
                   '/api/gb28181/device-mgmt/permission-workbench/assignments/departments',
                   '/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE `sys_api` SET `deleted_at`=NOW(), `updated_at`=NOW()
WHERE `deleted_at` IS NULL AND `path` IN ('/api/gb28181/device-mgmt/permission-workbench/summary',
                                         '/api/gb28181/device-mgmt/permission-workbench/devices/resolve',
                                         '/api/gb28181/device-mgmt/permission-workbench/grants/query',
                                         '/api/gb28181/device-mgmt/permission-workbench/grant-targets',
                                         '/api/gb28181/device-mgmt/permission-workbench/assignments',
                                         '/api/gb28181/device-mgmt/permission-workbench/assignments/departments',
                                         '/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE `sys_menu` SET `title`='设备分配', `updated_at`=NOW()
WHERE `name`='device-assignment' AND `title`='设备权限工作台' AND `deleted_at` IS NULL;
