SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_snapshot_session' AND INDEX_NAME = 'uk_cascade_snapshot_downstream') = 0,
  'CREATE UNIQUE INDEX `uk_cascade_snapshot_downstream` ON `gb_cascade_snapshot_session` (`downstream_session_id`)', 'SELECT 1');
PREPARE cascade_snapshot_route_stmt FROM @ddl;
EXECUTE cascade_snapshot_route_stmt;
DEALLOCATE PREPARE cascade_snapshot_route_stmt;
