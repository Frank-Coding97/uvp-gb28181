SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'config_enabled') > 0,
  'ALTER TABLE `gb_cascade_platform` DROP COLUMN `config_enabled`', 'SELECT 1');
PREPARE cascade_config_enabled_stmt FROM @ddl;
EXECUTE cascade_config_enabled_stmt;
DEALLOCATE PREPARE cascade_config_enabled_stmt;
