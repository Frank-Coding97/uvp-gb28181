IF COL_LENGTH(N'dbo.gb_device', N'device_type') IS NULL ALTER TABLE [dbo].[gb_device] ADD [device_type] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_device_device_type] DEFAULT N'ipc';
