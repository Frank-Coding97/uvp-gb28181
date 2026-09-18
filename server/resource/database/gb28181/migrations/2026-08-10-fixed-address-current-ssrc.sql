-- Persist the SSRC of the current live generation independently from stream_id.
-- MySQL 5.7+, repeatable.
SET @schema_name := DATABASE();
SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables
   WHERE table_schema = @schema_name AND table_name = 'gb_channel') = 1
  AND
  (SELECT COUNT(*) FROM information_schema.columns
   WHERE table_schema = @schema_name AND table_name = 'gb_channel'
     AND column_name = 'current_ssrc') = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `current_ssrc` varchar(10) NOT NULL DEFAULT '''' COMMENT ''当前实时媒体会话SSRC'' AFTER `stream_id`',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
