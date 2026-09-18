-- Roll back only firmware-upgrade API bindings and execution permission.
-- Preserve the durable audit table, its records, and shared maintenance-view permission.
DELETE FROM sys_casbin_rule WHERE ptype='p' AND ((v1='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND v2='GET'));
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:device:upgrade') OR api_id IN (SELECT id FROM sys_api WHERE (path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST') OR (path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET'));
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:device:upgrade');
DELETE FROM sys_menu WHERE permission='gb28181:device:upgrade';
DELETE FROM sys_api WHERE (path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST') OR (path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET');
