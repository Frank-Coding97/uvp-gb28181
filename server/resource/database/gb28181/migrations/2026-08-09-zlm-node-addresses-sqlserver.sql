-- 2026-08-09 ZLM 节点地址职责拆分。
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'receive_host') IS NULL
  ALTER TABLE [meta_node] ADD [receive_host] nvarchar(255) NOT NULL CONSTRAINT [df_meta_node_receive_host] DEFAULT N'';
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'playback_host') IS NULL
  ALTER TABLE [meta_node] ADD [playback_host] nvarchar(255) NOT NULL CONSTRAINT [df_meta_node_playback_host] DEFAULT N'';
