IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_cascade_media_session') AND name=N'idx_cascade_media_kind_state')
  DROP INDEX [idx_cascade_media_kind_state] ON [gb_cascade_media_session];
