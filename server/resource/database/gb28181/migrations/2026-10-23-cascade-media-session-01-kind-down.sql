SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_media_session' AND COLUMN_NAME = 'session_kind') > 0,
  'ALTER TABLE `gb_cascade_media_session` DROP COLUMN `session_kind`', 'SELECT 1');
PREPARE cascade_media_session_kind_down_stmt FROM @ddl;
EXECUTE cascade_media_session_kind_down_stmt;
DEALLOCATE PREPARE cascade_media_session_kind_down_stmt;
