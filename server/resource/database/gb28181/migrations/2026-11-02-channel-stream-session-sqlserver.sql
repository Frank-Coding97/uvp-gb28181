IF COL_LENGTH(N'dbo.gb_channel', N'current_stream_identification') IS NULL ALTER TABLE [dbo].[gb_channel] ADD [current_stream_identification] NVARCHAR(32) NULL;
