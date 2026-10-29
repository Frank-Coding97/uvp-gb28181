SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_snapshot_session' AND COLUMN_NAME = 'upload_token') = 0,
  'ALTER TABLE `gb_cascade_snapshot_session` ADD COLUMN `upload_token` varchar(64) NULL', 'SELECT 1');
PREPARE cascade_snapshot_route_stmt FROM @ddl;
EXECUTE cascade_snapshot_route_stmt;
DEALLOCATE PREPARE cascade_snapshot_route_stmt;
