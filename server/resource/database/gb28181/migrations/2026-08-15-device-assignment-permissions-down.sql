-- 回滚设备分配/共享授权 API 权限(MySQL)
DELETE FROM `sys_casbin_rule` WHERE `v1` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE ma FROM `sys_menu_api` ma JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE a.`path` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM `sys_api` WHERE `path` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
