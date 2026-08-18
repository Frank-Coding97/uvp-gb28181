-- 登录审计回滚,只删除本功能资产。
DELETE FROM `sys_casbin_rule` WHERE `v1` IN ('/api/sysLoginLog/list','/api/sysLoginLog/:id');
DELETE ma FROM `sys_menu_api` ma JOIN `sys_menu` m ON m.`id`=ma.`menu_id` WHERE m.`path`='/system/login-log';
DELETE FROM `sys_role_menu` WHERE `menu_id` IN (SELECT `id` FROM `sys_menu` WHERE `path`='/system/login-log');
DELETE FROM `sys_menu` WHERE `path`='/system/login-log';
DELETE FROM `sys_api` WHERE `path` IN ('/api/sysLoginLog/list','/api/sysLoginLog/:id');
DROP TABLE IF EXISTS `sys_login_logs`;
