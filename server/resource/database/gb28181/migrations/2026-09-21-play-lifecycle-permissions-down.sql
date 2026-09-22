-- Roll back playback lifecycle permissions (MySQL).
DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1 LIKE '/api/gb28181/play/lifecycles%';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/play/lifecycles%');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/playback-log');
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/playback-log' AND deleted_at IS NULL;
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path LIKE '/api/gb28181/play/lifecycles%' AND deleted_at IS NULL;
