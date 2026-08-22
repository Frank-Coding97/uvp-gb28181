IF COL_LENGTH(N'gb_device', N'zlm_node_id') IS NULL
    ALTER TABLE [gb_device] ADD [zlm_node_id] BIGINT NOT NULL CONSTRAINT [df_gb_device_zlm_node] DEFAULT 0;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE [name] = N'idx_gb_device_zlm_node' AND [object_id] = OBJECT_ID(N'gb_device'))
    CREATE INDEX [idx_gb_device_zlm_node] ON [gb_device] ([zlm_node_id]);
