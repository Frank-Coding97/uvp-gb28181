DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/gb28181/openapi-clients/capabilities/catalog' AND v2='GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/openapi-clients/capabilities/catalog' AND method='GET');
DELETE FROM sys_api WHERE path='/api/gb28181/openapi-clients/capabilities/catalog' AND method='GET';
