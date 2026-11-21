DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path=N'/api/system/upgrade/import/:upload_id' AND method='GET');
DELETE FROM sys_casbin_rule WHERE v1=N'/api/system/upgrade/import/:upload_id' AND v2='GET';
DELETE FROM sys_api WHERE path=N'/api/system/upgrade/import/:upload_id' AND method='GET';
