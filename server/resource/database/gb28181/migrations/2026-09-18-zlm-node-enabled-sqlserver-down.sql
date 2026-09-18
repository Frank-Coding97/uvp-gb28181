DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1 IN (N'/api/gb28181/zlm/nodes/:id/enable',N'/api/gb28181/zlm/nodes/:id/disable') AND v2='POST';
DELETE FROM sys_menu_api WHERE api_id IN (
  SELECT id FROM sys_api WHERE path IN (N'/api/gb28181/zlm/nodes/:id/enable',N'/api/gb28181/zlm/nodes/:id/disable') AND method=N'POST'
);
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path IN (N'/api/gb28181/zlm/nodes/:id/enable',N'/api/gb28181/zlm/nodes/:id/disable') AND method=N'POST' AND deleted_at IS NULL;
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'enabled') IS NOT NULL
  UPDATE [meta_node] SET [state]='maintenance' WHERE [enabled]=0;
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND EXISTS (SELECT 1 FROM sys.indexes WHERE name=N'idx_enabled' AND object_id=OBJECT_ID(N'meta_node'))
  DROP INDEX [idx_enabled] ON [meta_node];
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'enabled') IS NOT NULL
BEGIN
  IF OBJECT_ID(N'df_meta_node_enabled', N'D') IS NOT NULL ALTER TABLE [meta_node] DROP CONSTRAINT [df_meta_node_enabled];
  ALTER TABLE [meta_node] DROP COLUMN [enabled];
END;
