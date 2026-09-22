DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1=N'/api/gb28181/openapi-clients/capabilities/catalog' AND v2=N'GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/capabilities/catalog' AND method=N'GET');
DELETE FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/capabilities/catalog' AND method=N'GET';
