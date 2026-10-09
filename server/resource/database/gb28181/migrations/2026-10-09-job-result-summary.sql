SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_job_results' AND COLUMN_NAME = 'summary') = 0,
  'ALTER TABLE `sys_job_results` ADD COLUMN `summary` TEXT NULL', 'SELECT 1');
PREPARE sys_job_results_summary_stmt FROM @ddl;
EXECUTE sys_job_results_summary_stmt;
DEALLOCATE PREPARE sys_job_results_summary_stmt;
