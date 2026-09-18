-- 2026-07-25: persist the user-selected GB28181 audio session mode.
-- MySQL 5.7+; guard both the table and column so repeated upgrades are safe.

SET @schema_name := DATABASE();

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_talk_session') = 1
  AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_talk_session' AND column_name = 'mode') = 0,
  'ALTER TABLE `gb_talk_session` ADD COLUMN `mode` varchar(16) NOT NULL DEFAULT ''talk'' AFTER `actor_dept_id`',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_talk_session') = 1,
  'UPDATE `gb_talk_session` SET `mode` = ''talk'' WHERE `mode` IS NULL OR `mode` NOT IN (''broadcast'', ''talk'')',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
