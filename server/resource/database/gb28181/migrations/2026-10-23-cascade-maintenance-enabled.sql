SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'maintenance_enabled') = 0,
  'ALTER TABLE `gb_cascade_platform` ADD COLUMN `maintenance_enabled` tinyint(1) NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE cascade_maintenance_enabled_stmt FROM @ddl;
EXECUTE cascade_maintenance_enabled_stmt;
DEALLOCATE PREPARE cascade_maintenance_enabled_stmt;
