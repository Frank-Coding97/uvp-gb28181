DROP TABLE IF EXISTS `gb_play_lifecycle_event`;
SET @attempt_exists := (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='gb_play_attempt');
SET @lifecycle_columns_exist := (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='gb_play_attempt' AND column_name='lifecycle_state');
SET @sql := IF(@attempt_exists=1 AND @lifecycle_columns_exist=1,
  'ALTER TABLE `gb_play_attempt`
    DROP INDEX `idx_play_attempt_lifecycle_started`,
    DROP INDEX `idx_play_attempt_stream_node`,
    DROP COLUMN `last_event_at`, DROP COLUMN `client_error_code`, DROP COLUMN `client_error_at`, DROP COLUMN `client_first_frame_at`,
    DROP COLUMN `reason_message`, DROP COLUMN `reason_code`, DROP COLUMN `lifecycle_state`, DROP COLUMN `client_state`,
    DROP COLUMN `media_state`, DROP COLUMN `current_stage`, DROP COLUMN `cseq`, DROP COLUMN `call_id`, DROP COLUMN `ssrc`, DROP COLUMN `stream_id`',
  'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
