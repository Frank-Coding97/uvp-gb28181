SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'status_event') > 0,
  'ALTER TABLE `gb_channel` DROP COLUMN `status_event`', 'SELECT 1');
PREPARE cascade_status_event_stmt FROM @ddl;
EXECUTE cascade_status_event_stmt;
DEALLOCATE PREPARE cascade_status_event_stmt;
