IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'sys_job_results') AND name=N'idx_created_at')
  CREATE INDEX [idx_created_at] ON [sys_job_results] ([created_at]);
