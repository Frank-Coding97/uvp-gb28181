IF COL_LENGTH(N'dbo.gb_catalog_node', N'source_device_id') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_catalog_node]
    ADD [source_device_id] NVARCHAR(20) NOT NULL CONSTRAINT [df_gb_catalog_node_source_device_id] DEFAULT (N'');
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.gb_catalog_node') AND name=N'idx_owner_source_type_code')
  CREATE INDEX [idx_owner_source_type_code] ON [dbo].[gb_catalog_node] ([owner_dept_id], [source_device_id], [node_type], [code]);
-- 可由物理关联确定来源的历史节点安全回填；无法唯一归因的业务节点保留空值。
UPDATE n
SET n.[source_device_id] = c.[device_id]
FROM [dbo].[gb_catalog_node] n
JOIN [dbo].[gb_channel] c ON c.[id] = n.[channel_id]
WHERE n.[source_device_id] = N'' AND c.[device_id] <> N''
  AND n.[node_type] = N'channel' AND n.[source] = N'catalog'
  AND n.[owner_dept_id] = c.[owner_dept_id]
  AND n.[deleted_at] IS NULL AND c.[deleted_at] IS NULL;
