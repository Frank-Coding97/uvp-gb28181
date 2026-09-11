-- Safe rollback gate: keep the column when any live stream state is present.
SET @schema_name := DATABASE();
SET @column_exists := (
  SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema = @schema_name AND table_name = 'gb_channel'
    AND column_name = 'current_ssrc'
);
SET @active_count := 0;
SET @sql := IF(
  @column_exists = 1,
  'SELECT COUNT(*) INTO @active_count FROM `gb_channel` WHERE `stream_id` <> '''' OR `current_ssrc` <> ''''',
  'SELECT 0 INTO @active_count'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  @column_exists = 1 AND @active_count = 0,
  'ALTER TABLE `gb_channel` DROP COLUMN `current_ssrc`',
  'SELECT ''current_ssrc rollback skipped: active stream state exists or column is absent'' AS rollback_status'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
