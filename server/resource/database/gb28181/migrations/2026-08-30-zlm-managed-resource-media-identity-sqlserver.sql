-- Add the complete non-secret ZLM media identity to the management ledger.
-- Existing rows intentionally remain empty/unknown; no identity is inferred.
IF OBJECT_ID(N'gb_zlm_managed_resource', N'U') IS NOT NULL AND COL_LENGTH(N'gb_zlm_managed_resource', N'schema') IS NULL
  ALTER TABLE [gb_zlm_managed_resource] ADD [schema] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_zlm_managed_resource_schema] DEFAULT N''
IF OBJECT_ID(N'gb_zlm_managed_resource', N'U') IS NOT NULL AND COL_LENGTH(N'gb_zlm_managed_resource', N'vhost') IS NULL
  ALTER TABLE [gb_zlm_managed_resource] ADD [vhost] NVARCHAR(128) NOT NULL CONSTRAINT [df_gb_zlm_managed_resource_vhost] DEFAULT N''

-- The original key omitted media identity. Replace it with the exact tuple.
DECLARE @managed_resource_unique_matches BIT = 0
IF OBJECT_ID(N'gb_zlm_managed_resource', N'U') IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM sys.indexes ix
    WHERE ix.object_id=OBJECT_ID(N'gb_zlm_managed_resource')
      AND ix.name=N'uk_gb_zlm_managed_resource_identity'
      AND ix.is_unique=1
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=1 AND c.name=N'node_id')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=2 AND c.name=N'resource_type')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=3 AND c.name=N'resource_key')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=4 AND c.name=N'schema')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=5 AND c.name=N'vhost')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=6 AND c.name=N'app')
      AND EXISTS (SELECT 1 FROM sys.index_columns ic JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal=7 AND c.name=N'stream')
      AND (SELECT COUNT(*) FROM sys.index_columns ic WHERE ic.object_id=ix.object_id AND ic.index_id=ix.index_id AND ic.key_ordinal>0)=7
  )
  SET @managed_resource_unique_matches = 1

IF @managed_resource_unique_matches=0 AND OBJECT_ID(N'gb_zlm_managed_resource', N'U') IS NOT NULL
BEGIN
  IF EXISTS (SELECT 1 FROM sys.key_constraints WHERE parent_object_id=OBJECT_ID(N'gb_zlm_managed_resource') AND name=N'uk_gb_zlm_managed_resource_identity')
    ALTER TABLE [gb_zlm_managed_resource] DROP CONSTRAINT [uk_gb_zlm_managed_resource_identity]
  IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_zlm_managed_resource') AND name=N'uk_gb_zlm_managed_resource_identity')
    DROP INDEX [uk_gb_zlm_managed_resource_identity] ON [gb_zlm_managed_resource]
  CREATE UNIQUE INDEX [uk_gb_zlm_managed_resource_identity]
    ON [gb_zlm_managed_resource]([node_id],[resource_type],[resource_key],[schema],[vhost],[app],[stream])
END
