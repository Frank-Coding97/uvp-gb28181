-- 通道坐标来源标记 gb_channel.position_source / position_updated_at —— SQL Server 方言.
-- 与 2026-09-21-channel-position-source.sql 等价；语义说明见该文件。

IF COL_LENGTH(N'gb_channel', N'position_source') IS NULL
    ALTER TABLE [gb_channel] ADD [position_source] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_channel_position_source] DEFAULT N'';

IF COL_LENGTH(N'gb_channel', N'position_updated_at') IS NULL
    ALTER TABLE [gb_channel] ADD [position_updated_at] DATETIME2(3) NULL;

-- 存量数据回填：把已有非零坐标标成 catalog（此前只有目录这一条通路）。
-- ⛔ 只在 position_source 为空时回填，重跑不覆盖 mobile/manual。
UPDATE [gb_channel]
SET [position_source] = N'catalog'
WHERE [position_source] = N'' AND ([longitude] <> 0 OR [latitude] <> 0);
