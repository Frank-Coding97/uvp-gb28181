DELETE FROM `sys_casbin_rule` WHERE `v1` LIKE '/api/gb28181/channel-favorite-groups%';
DELETE ma FROM `sys_menu_api` ma JOIN `sys_menu` m ON m.`id`=ma.`menu_id` WHERE m.`permission`='gb28181:channel-favorite:manage';
DELETE rm FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` WHERE m.`permission`='gb28181:channel-favorite:manage';
DELETE FROM `sys_menu` WHERE `permission`='gb28181:channel-favorite:manage';
DELETE FROM `sys_api` WHERE `path` LIKE '/api/gb28181/channel-favorite-groups%';
