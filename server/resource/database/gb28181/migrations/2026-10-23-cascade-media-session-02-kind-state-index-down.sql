SET @ddl := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'gb_cascade_media_session' AND index_name = 'idx_cascade_media_kind_state') > 0,
  'DROP INDEX `idx_cascade_media_kind_state` ON `gb_cascade_media_session`', 'SELECT 1');
PREPARE cascade_media_session_kind_index_down_stmt FROM @ddl;
EXECUTE cascade_media_session_kind_index_down_stmt;
DEALLOCATE PREPARE cascade_media_session_kind_index_down_stmt;
