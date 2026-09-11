-- Remove only device-maintenance permissions and API registrations.
DELETE FROM sys_casbin_rule WHERE ptype='p' AND ((v1='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/device/:id/reboot' AND v2='POST'));
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission IN ('gb28181:device:maintenance:view','gb28181:device:reboot')) OR api_id IN (SELECT id FROM sys_api WHERE (path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET') OR (path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST'));
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission IN ('gb28181:device:maintenance:view','gb28181:device:reboot'));
DELETE FROM sys_menu WHERE permission IN ('gb28181:device:maintenance:view','gb28181:device:reboot');
DELETE FROM sys_api WHERE (path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET') OR (path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST');
