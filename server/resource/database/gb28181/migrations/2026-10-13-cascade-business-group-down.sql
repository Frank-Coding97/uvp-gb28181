DELETE FROM sys_casbin_rule WHERE ptype='p' AND v0='role_1' AND v1='/api/gb28181/cascade/platforms/:id/business-group' AND v2 IN ('GET','PUT');
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/business-group' AND method IN ('GET','PUT'));
DELETE FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/business-group' AND method IN ('GET','PUT');
DROP TABLE IF EXISTS gb_cascade_business_binding;
DROP TABLE IF EXISTS gb_cascade_business_node;
