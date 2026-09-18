DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1 IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND v2='POST';
DELETE FROM sys_menu_api WHERE api_id IN (
  SELECT id FROM sys_api WHERE path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND method='POST'
);
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path IN ('/api/gb28181/zlm/nodes/:id/enable','/api/gb28181/zlm/nodes/:id/disable') AND method='POST' AND deleted_at IS NULL;
UPDATE meta_node SET state='maintenance' WHERE enabled=FALSE;
DROP INDEX IF EXISTS idx_enabled;
ALTER TABLE IF EXISTS meta_node DROP COLUMN IF EXISTS enabled;
