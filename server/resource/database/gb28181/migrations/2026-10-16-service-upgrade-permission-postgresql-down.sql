DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE '/api/system/upgrade/%');
DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/system/upgrade/%';
DELETE FROM sys_api WHERE path LIKE '/api/system/upgrade/%';
DELETE FROM sys_menu WHERE permission IN ('system:upgrade:read','system:upgrade:manage');
DELETE FROM sys_menu WHERE path='/system/service-upgrade/index' AND name='service-upgrade';
