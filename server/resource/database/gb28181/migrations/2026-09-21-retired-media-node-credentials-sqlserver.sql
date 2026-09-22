-- 媒体节点退役：保留「撤销对端 hook」所需的凭据（SQL Server）。
-- 与 MySQL 版逐字对应，只换方言；语义说明见 2026-09-21-retired-media-node-credentials.sql。

-- retired-media-node-credentials:start

IF OBJECT_ID(N'meta_node_retired', N'U') IS NULL
BEGIN
    CREATE TABLE [meta_node_retired] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [media_server_uuid] NVARCHAR(64) NOT NULL,
        [name] NVARCHAR(64) NOT NULL CONSTRAINT [df_retired_node_name] DEFAULT N'',
        [host] NVARCHAR(64) NOT NULL CONSTRAINT [df_retired_node_host] DEFAULT N'',
        [api_port] INT NOT NULL CONSTRAINT [df_retired_node_api_port] DEFAULT 18080,
        [api_secret] NVARCHAR(128) NOT NULL CONSTRAINT [df_retired_node_api_secret] DEFAULT N'',
        [retire_reason] NVARCHAR(255) NOT NULL CONSTRAINT [df_retired_node_reason] DEFAULT N'',
        [unprovision_state] NVARCHAR(16) NOT NULL CONSTRAINT [df_retired_node_state] DEFAULT N'pending',
        [unprovision_attempts] INT NOT NULL CONSTRAINT [df_retired_node_attempts] DEFAULT 0,
        [last_attempt_at] DATETIME2(6),
        [retired_at] DATETIME2(6) NOT NULL,
        [updated_at] DATETIME2(6) NOT NULL,
        CONSTRAINT [pk_retired_media_node] PRIMARY KEY ([id]),
        CONSTRAINT [uk_retired_media_server_uuid] UNIQUE ([media_server_uuid])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'meta_node_retired') AND name = N'idx_retired_unprovision_state')
    CREATE INDEX [idx_retired_unprovision_state] ON [meta_node_retired] ([unprovision_state]);

-- retired-media-node-credentials:end
