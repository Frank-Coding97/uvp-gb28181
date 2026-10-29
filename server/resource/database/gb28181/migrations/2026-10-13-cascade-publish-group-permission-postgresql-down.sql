DELETE FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/cascade/platforms/:id/publish-group' AND v2='PUT';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/publish-group' AND method='PUT');
DELETE FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/publish-group' AND method='PUT';
