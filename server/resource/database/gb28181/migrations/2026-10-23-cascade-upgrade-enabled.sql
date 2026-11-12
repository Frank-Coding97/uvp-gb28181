SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'upgrade_enabled') = 0,
  'ALTER TABLE `gb_cascade_platform` ADD COLUMN `upgrade_enabled` tinyint(1) NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE cascade_upgrade_enabled_stmt FROM @ddl;
EXECUTE cascade_upgrade_enabled_stmt;
DEALLOCATE PREPARE cascade_upgrade_enabled_stmt;
