SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'device_control_enabled') = 0,
  'ALTER TABLE `gb_cascade_platform` ADD COLUMN `device_control_enabled` tinyint(1) NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE cascade_device_control_enabled_stmt FROM @ddl;
EXECUTE cascade_device_control_enabled_stmt;
DEALLOCATE PREPARE cascade_device_control_enabled_stmt;
