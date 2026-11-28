DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE BINARY path LIKE BINARY '/api/system/upgrade/%');
DELETE FROM sys_casbin_rule WHERE BINARY v1 LIKE BINARY '/api/system/upgrade/%';
DELETE FROM sys_api WHERE BINARY path LIKE BINARY '/api/system/upgrade/%';
DELETE FROM sys_menu WHERE BINARY permission IN (BINARY 'system:upgrade:read',BINARY 'system:upgrade:manage');
DELETE FROM sys_menu WHERE BINARY path=BINARY '/system/service-upgrade/index' AND BINARY name=BINARY 'service-upgrade';
