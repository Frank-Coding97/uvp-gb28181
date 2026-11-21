-- sys_job_results.idx_created_at 是死索引：列表只按 job_id/status/start_time 过滤，
-- 清理按 end_time 删除，仓储层从来没有按 created_at 查过这张表。
-- 它却要为每次任务结果写入多维护一棵 B 树，并在盘上白占 26MB（线上实测）。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.statistics
                WHERE table_schema = DATABASE() AND table_name = 'sys_job_results'
                  AND index_name = 'idx_created_at') > 0,
  'DROP INDEX `idx_created_at` ON `sys_job_results`', 'SELECT 1');
PREPARE job_result_drop_created_at_stmt FROM @ddl;
EXECUTE job_result_drop_created_at_stmt;
DEALLOCATE PREPARE job_result_drop_created_at_stmt;
