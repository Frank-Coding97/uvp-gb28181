SET @ddl := IF((SELECT COUNT(*) FROM information_schema.statistics
                WHERE table_schema = DATABASE() AND table_name = 'sys_job_results'
                  AND index_name = 'idx_created_at') = 0,
  'ALTER TABLE `sys_job_results` ADD INDEX `idx_created_at` (`created_at`)', 'SELECT 1');
PREPARE job_result_add_created_at_stmt FROM @ddl;
EXECUTE job_result_add_created_at_stmt;
DEALLOCATE PREPARE job_result_add_created_at_stmt;
