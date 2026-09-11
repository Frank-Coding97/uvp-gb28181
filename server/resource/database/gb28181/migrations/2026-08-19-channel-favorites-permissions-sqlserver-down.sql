DELETE FROM sys_casbin_rule WHERE v1 LIKE N'/api/gb28181/channel-favorite-groups%';
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission=N'gb28181:channel-favorite:manage');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission=N'gb28181:channel-favorite:manage');
DELETE FROM sys_menu WHERE permission=N'gb28181:channel-favorite:manage';
DELETE FROM sys_api WHERE path LIKE N'/api/gb28181/channel-favorite-groups%';
