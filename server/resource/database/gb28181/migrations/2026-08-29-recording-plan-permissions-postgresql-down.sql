DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/recording-plans%';
DELETE FROM sys_menu_api ma USING sys_api a WHERE a.id=ma.api_id AND a.path LIKE '/api/gb28181/recording-plans%';
DELETE FROM sys_role_menu rm USING sys_menu m WHERE m.id=rm.menu_id AND m.permission IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign');
DELETE FROM sys_menu WHERE permission IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign');
DELETE FROM sys_api WHERE path LIKE '/api/gb28181/recording-plans%';
