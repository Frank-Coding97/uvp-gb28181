IF COL_LENGTH(N'dbo.gb_cascade_snapshot_session', N'downstream_session_id') IS NULL
  ALTER TABLE [dbo].[gb_cascade_snapshot_session] ADD [downstream_session_id] NVARCHAR(128) NULL;
