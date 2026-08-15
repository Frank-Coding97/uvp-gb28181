-- Safe rollback removes access metadata but retains collected traffic data.
DELETE FROM `sys_casbin_rule` WHERE `v1` LIKE '/api/gb28181/device-traffic/%';
DELETE ma FROM `sys_menu_api` ma JOIN `sys_api` a ON a.`id`=ma.`api_id` WHERE a.`path` LIKE '/api/gb28181/device-traffic/%';
DELETE rm FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` WHERE m.`permission`='gb28181:traffic:view';
DELETE FROM `sys_menu` WHERE `permission`='gb28181:traffic:view';
DELETE FROM `sys_api` WHERE `path` LIKE '/api/gb28181/device-traffic/%';
