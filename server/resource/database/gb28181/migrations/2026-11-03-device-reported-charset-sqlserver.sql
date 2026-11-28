IF COL_LENGTH(N'dbo.gb_device', N'reported_charset') IS NULL ALTER TABLE [dbo].[gb_device] ADD [reported_charset] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_device_reported_charset] DEFAULT N'';
IF COL_LENGTH(N'dbo.gb_device', N'wire_charset') IS NULL ALTER TABLE [dbo].[gb_device] ADD [wire_charset] NVARCHAR(32) NOT NULL CONSTRAINT [df_gb_device_wire_charset] DEFAULT N'';
IF COL_LENGTH(N'dbo.gb_device', N'reported_charset_at') IS NULL ALTER TABLE [dbo].[gb_device] ADD [reported_charset_at] DATETIME2(3) NULL;
