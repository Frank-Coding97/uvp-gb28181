IF COL_LENGTH(N'dbo.gb_cascade_snapshot_session', N'upstream_upload_url') IS NULL
  ALTER TABLE [dbo].[gb_cascade_snapshot_session] ADD [upstream_upload_url] NVARCHAR(MAX) NULL;
