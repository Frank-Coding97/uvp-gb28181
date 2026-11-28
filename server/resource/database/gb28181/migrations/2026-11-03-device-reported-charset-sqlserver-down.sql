IF COL_LENGTH(N'dbo.gb_device', N'reported_charset') IS NOT NULL ALTER TABLE [dbo].[gb_device] DROP COLUMN [reported_charset];
IF COL_LENGTH(N'dbo.gb_device', N'wire_charset') IS NOT NULL ALTER TABLE [dbo].[gb_device] DROP COLUMN [wire_charset];
IF COL_LENGTH(N'dbo.gb_device', N'reported_charset_at') IS NOT NULL ALTER TABLE [dbo].[gb_device] DROP COLUMN [reported_charset_at];
