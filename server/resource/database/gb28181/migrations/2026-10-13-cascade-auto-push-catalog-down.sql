SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'auto_push_catalog') > 0,
  'ALTER TABLE `gb_cascade_platform` DROP COLUMN `auto_push_catalog`', 'SELECT 1');
PREPARE cascade_auto_push_catalog_stmt FROM @ddl;
EXECUTE cascade_auto_push_catalog_stmt;
DEALLOCATE PREPARE cascade_auto_push_catalog_stmt;
