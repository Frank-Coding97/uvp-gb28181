SET @ddl := IF((SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_cascade_media_session' AND COLUMN_NAME = 'session_kind') = 0,
  'ALTER TABLE `gb_cascade_media_session` ADD COLUMN `session_kind` varchar(16) NOT NULL DEFAULT ''video''', 'SELECT 1');
PREPARE cascade_media_session_kind_stmt FROM @ddl;
EXECUTE cascade_media_session_kind_stmt;
DEALLOCATE PREPARE cascade_media_session_kind_stmt;
