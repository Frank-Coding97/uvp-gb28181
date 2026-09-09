DELETE c FROM sys_casbin_rule c WHERE c.v1='/api/gb28181/logs/stream' AND c.v2='GET';
DELETE ma FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission='gb28181:log:view';
DELETE rm FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.permission='gb28181:log:view';
DELETE FROM sys_menu WHERE permission='gb28181:log:view' OR path='/gb28181/realtime-log';
DELETE FROM sys_api WHERE path='/api/gb28181/logs/stream' AND method='GET';
