SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_platform' AND COLUMN_NAME = 'rtcp_keepalive') > 0,
  'ALTER TABLE `gb_cascade_platform` DROP COLUMN `rtcp_keepalive`', 'SELECT 1');
PREPARE cascade_rtcp_keepalive_stmt FROM @ddl;
EXECUTE cascade_rtcp_keepalive_stmt;
DEALLOCATE PREPARE cascade_rtcp_keepalive_stmt;
