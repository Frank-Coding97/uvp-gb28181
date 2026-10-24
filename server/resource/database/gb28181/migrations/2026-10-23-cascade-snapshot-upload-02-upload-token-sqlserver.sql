IF COL_LENGTH(N'dbo.gb_cascade_snapshot_session', N'upload_token') IS NULL
  ALTER TABLE [dbo].[gb_cascade_snapshot_session] ADD [upload_token] NVARCHAR(64) NULL;
