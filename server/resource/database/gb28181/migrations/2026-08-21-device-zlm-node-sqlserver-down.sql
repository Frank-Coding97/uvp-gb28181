IF EXISTS (SELECT 1 FROM sys.indexes WHERE [name] = N'idx_gb_device_zlm_node' AND [object_id] = OBJECT_ID(N'gb_device'))
    DROP INDEX [idx_gb_device_zlm_node] ON [gb_device];
IF COL_LENGTH(N'gb_device', N'zlm_node_id') IS NOT NULL
BEGIN
    IF OBJECT_ID(N'df_gb_device_zlm_node', N'D') IS NOT NULL
        ALTER TABLE [gb_device] DROP CONSTRAINT [df_gb_device_zlm_node];
    ALTER TABLE [gb_device] DROP COLUMN [zlm_node_id];
END;
