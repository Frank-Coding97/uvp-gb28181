DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/logs/stream' AND v2='GET';
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:log:view');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:log:view');
DELETE FROM sys_menu WHERE permission='gb28181:log:view' OR path='/gb28181/realtime-log';
DELETE FROM sys_api WHERE path='/api/gb28181/logs/stream' AND method='GET';
