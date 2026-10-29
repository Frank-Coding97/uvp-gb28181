IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.gb_cascade_snapshot_session') AND name=N'uk_cascade_snapshot_upload')
  CREATE UNIQUE INDEX [uk_cascade_snapshot_upload] ON [dbo].[gb_cascade_snapshot_session] ([upload_token]) WHERE [upload_token] IS NOT NULL;
