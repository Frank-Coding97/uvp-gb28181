IF COL_LENGTH(N'dbo.gb_channel', N'status_event') IS NULL ALTER TABLE [dbo].[gb_channel] ADD [status_event] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_channel_status_event] DEFAULT N'';
