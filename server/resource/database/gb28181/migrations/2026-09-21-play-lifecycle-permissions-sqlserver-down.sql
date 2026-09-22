-- Roll back playback lifecycle permissions (SQL Server).
DELETE FROM sys_casbin_rule WHERE ptype=N'p' AND v1 LIKE N'/api/gb28181/play/lifecycles%';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE N'/api/gb28181/play/lifecycles%');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path=N'/gb28181/playback-log');
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path=N'/gb28181/playback-log' AND deleted_at IS NULL;
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path LIKE N'/api/gb28181/play/lifecycles%' AND deleted_at IS NULL;
