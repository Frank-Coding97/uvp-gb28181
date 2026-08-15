-- Safe rollback removes access metadata but retains collected traffic data.
DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/device-traffic/%';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/device-traffic/%');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:traffic:view');
DELETE FROM sys_menu WHERE permission='gb28181:traffic:view';
DELETE FROM sys_api WHERE path LIKE '/api/gb28181/device-traffic/%';
