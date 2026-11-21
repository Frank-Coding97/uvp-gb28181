IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.gb_catalog_node') AND name=N'idx_owner_source_type_code')
  DROP INDEX [idx_owner_source_type_code] ON [dbo].[gb_catalog_node];
IF COL_LENGTH(N'dbo.gb_catalog_node', N'source_device_id') IS NOT NULL
  ALTER TABLE [dbo].[gb_catalog_node] DROP CONSTRAINT [df_gb_catalog_node_source_device_id];
IF COL_LENGTH(N'dbo.gb_catalog_node', N'source_device_id') IS NOT NULL
  ALTER TABLE [dbo].[gb_catalog_node] DROP COLUMN [source_device_id];
