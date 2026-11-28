-- 同 MySQL 版：这张表的 created_at 索引没有任何查询用到。
-- ⛔ SQLite 的索引名是全库唯一的，而 sys_jobs 也声明了一个同名 idx_created_at
--   （基线里第二个 CREATE INDEX IF NOT EXISTS 因此被静默跳过）。
--   全库唯一的那一个建在 sys_job_results 上，直接删即可；删掉后 sys_jobs
--   本来就没有这个索引，不构成回归。
DROP INDEX IF EXISTS "idx_created_at";
