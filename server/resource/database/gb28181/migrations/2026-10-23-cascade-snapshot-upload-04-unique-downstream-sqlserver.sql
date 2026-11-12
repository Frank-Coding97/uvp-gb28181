IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.gb_cascade_snapshot_session') AND name=N'uk_cascade_snapshot_downstream')
  CREATE UNIQUE INDEX [uk_cascade_snapshot_downstream] ON [dbo].[gb_cascade_snapshot_session] ([downstream_session_id]) WHERE [downstream_session_id] IS NOT NULL;
