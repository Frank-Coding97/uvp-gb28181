-- 回滚 2026-09-21-channel-position-source-sqlserver.sql（SQL Server）。
-- 语义与 MySQL 版 down 一致，见 2026-09-21-channel-position-source-down.sql。

IF COL_LENGTH(N'gb_channel', N'position_updated_at') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [position_updated_at];

IF COL_LENGTH(N'gb_channel', N'position_source') IS NOT NULL
    ALTER TABLE [gb_channel] DROP COLUMN [position_source];
