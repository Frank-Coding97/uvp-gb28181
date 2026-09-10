-- Persist the original recorder owner and exact media identity.
-- Existing rows receive neutral defaults and are never adopted automatically.
-- MySQL 5.7+, repeatable.
SET @schema_name := DATABASE();

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recording_plan_channel_state' AND column_name = 'recorder_owner_kind') = 0,
  'ALTER TABLE `gb_recording_plan_channel_state` ADD COLUMN `recorder_owner_kind` varchar(20) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recording_plan_channel_state' AND column_name = 'recorder_owner_id') = 0,
  'ALTER TABLE `gb_recording_plan_channel_state` ADD COLUMN `recorder_owner_id` varchar(128) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recording_plan_channel_state' AND column_name = 'recorder_claim_version') = 0,
  'ALTER TABLE `gb_recording_plan_channel_state` ADD COLUMN `recorder_claim_version` bigint unsigned NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'channel_id') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `channel_id` bigint NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'node_id') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `node_id` bigint NOT NULL DEFAULT 0',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'v_host') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `v_host` varchar(128) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'app') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `app` varchar(64) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_recorder_claim' AND column_name = 'stream') = 0,
  'ALTER TABLE `gb_recorder_claim` ADD COLUMN `stream` varchar(64) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
