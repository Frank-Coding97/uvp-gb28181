SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'recording_enabled') > 0,
  'ALTER TABLE `gb_cascade_platform` DROP COLUMN `recording_enabled`', 'SELECT 1');
PREPARE cascade_recording_enabled_stmt FROM @ddl;
EXECUTE cascade_recording_enabled_stmt;
DEALLOCATE PREPARE cascade_recording_enabled_stmt;
