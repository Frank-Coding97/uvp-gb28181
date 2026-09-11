-- cleanup 1: sys_casbin_rule
DELETE FROM sys_casbin_rule WHERE ptype='p' AND ((v1='/api/sysOnlineUser/list' AND v2='GET') OR (v1='/api/sysOnlineUser/forceLogout' AND v2='POST'));
-- cleanup 2: sys_menu_api
DELETE FROM sys_menu_api ma USING sys_api a WHERE a.id=ma.api_id AND ((a.path='/api/sysOnlineUser/list' AND a.method='GET') OR (a.path='/api/sysOnlineUser/forceLogout' AND a.method='POST'));
-- cleanup 3: sys_role_menu
DELETE FROM sys_role_menu rm USING sys_menu m WHERE m.id=rm.menu_id AND (m.path='/system/online-user' OR m.permission IN ('system:online-user:list','system:online-user:force-logout'));
-- cleanup 4: sys_menu
DELETE FROM sys_menu WHERE path='/system/online-user' OR permission IN ('system:online-user:list','system:online-user:force-logout');
-- cleanup 5: sys_api
DELETE FROM sys_api WHERE (path='/api/sysOnlineUser/list' AND method='GET') OR (path='/api/sysOnlineUser/forceLogout' AND method='POST');
