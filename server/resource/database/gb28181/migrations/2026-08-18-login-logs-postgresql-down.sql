DELETE FROM sys_casbin_rule WHERE v1 IN ('/api/sysLoginLog/list','/api/sysLoginLog/:id','/api/sysLoginLog/delete','/api/sysLoginLog/clear','/api/sysLoginLog/unlock');
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/system/login-log');
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission IN ('system:login-log:delete','system:login-log:clear','system:login-log:unlock'));
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission IN ('system:login-log:delete','system:login-log:clear','system:login-log:unlock'));
DELETE FROM sys_menu WHERE permission IN ('system:login-log:delete','system:login-log:clear','system:login-log:unlock');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/system/login-log');
DELETE FROM sys_menu WHERE path='/system/login-log';
DELETE FROM sys_api WHERE path IN ('/api/sysLoginLog/list','/api/sysLoginLog/:id','/api/sysLoginLog/delete','/api/sysLoginLog/clear','/api/sysLoginLog/unlock');
DROP TABLE IF EXISTS sys_login_logs;
