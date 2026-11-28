-- 同 MySQL 版：这张表的 created_at 索引没有任何查询用到。
-- PostgreSQL 的索引名由生成器加表名前缀，不是 idx_created_at。
DROP INDEX IF EXISTS "sys_job_results__idx_created_at";
