DELETE FROM sys_casbin_rule WHERE v1 IN (N'/api/sysLoginLog/list',N'/api/sysLoginLog/:id');
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE path=N'/system/login-log');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path=N'/system/login-log');
DELETE FROM sys_menu WHERE path=N'/system/login-log';
DELETE FROM sys_api WHERE path IN (N'/api/sysLoginLog/list',N'/api/sysLoginLog/:id');
IF OBJECT_ID(N'sys_login_logs', N'U') IS NOT NULL DROP TABLE sys_login_logs;
