DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/system/upgrade/migration-plan' AND method='GET');
DELETE FROM sys_casbin_rule WHERE v1='/api/system/upgrade/migration-plan' AND v2='GET';
DELETE FROM sys_api WHERE path='/api/system/upgrade/migration-plan' AND method='GET';
