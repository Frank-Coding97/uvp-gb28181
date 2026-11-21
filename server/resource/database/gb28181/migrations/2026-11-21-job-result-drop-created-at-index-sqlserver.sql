-- 同 MySQL 版：这张表的 created_at 索引没有任何查询用到。
IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'sys_job_results') AND name=N'idx_created_at')
  DROP INDEX [idx_created_at] ON [sys_job_results];
