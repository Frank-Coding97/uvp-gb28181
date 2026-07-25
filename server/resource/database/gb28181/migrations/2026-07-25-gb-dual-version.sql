-- 2026-07-25: GB28181 2016/2022 profile archive and control-state facts.
-- MySQL 5.7+; every ALTER is guarded for repeatable upgrades.

SET @schema_name := DATABASE();

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'reported_version') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `reported_version` varchar(8) NOT NULL DEFAULT '''' COMMENT ''last X-GB-Ver''', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'reported_version_at') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `reported_version_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'protocol_override') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `protocol_override` varchar(8) NOT NULL DEFAULT ''auto''', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'effective_version') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `effective_version` varchar(8) NOT NULL DEFAULT ''2016''', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'effective_version_source') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `effective_version_source` varchar(16) NOT NULL DEFAULT ''default''', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_device' AND column_name = 'effective_version_at') = 0,
  'ALTER TABLE `gb_device` ADD COLUMN `effective_version_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'profile_version') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `profile_version` varchar(8) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'profile_charset') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `profile_charset` varchar(16) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'target_scope') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `target_scope` varchar(16) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'target_code') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `target_code` varchar(20) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS `gb_device_control_state` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL DEFAULT 0,
  `target_scope` varchar(16) NOT NULL,
  `target_code` varchar(20) NOT NULL,
  `record_state` varchar(8) NOT NULL DEFAULT 'unknown',
  `guard_state` varchar(8) NOT NULL DEFAULT 'unknown',
  `freshness` varchar(8) NOT NULL DEFAULT 'unknown',
  `observed_at` datetime(3) NOT NULL,
  `source` varchar(32) NOT NULL DEFAULT 'device_status',
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_control_state_target` (`target_scope`, `target_code`),
  KEY `idx_control_state_device_target` (`device_id`, `target_scope`, `target_code`),
  KEY `idx_control_state_channel` (`channel_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_target') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_target` (`device_code`,`target_scope`,`target_code`,`status`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

UPDATE `gb_device`
SET `protocol_override` = COALESCE(NULLIF(`protocol_override`, ''), 'auto'),
    `effective_version` = COALESCE(NULLIF(`effective_version`, ''), '2016'),
    `effective_version_source` = COALESCE(NULLIF(`effective_version_source`, ''), 'default')
WHERE `protocol_override` = '' OR `effective_version` = '' OR `effective_version_source` = '';

UPDATE `gb_ptz_operation`
SET `profile_version` = COALESCE(NULLIF(`profile_version`, ''), '2016'),
    `profile_charset` = COALESCE(NULLIF(`profile_charset`, ''), 'GB2312'),
    `target_scope` = COALESCE(NULLIF(`target_scope`, ''), 'channel'),
    `target_code` = COALESCE(NULLIF(`target_code`, ''), `channel_code`)
WHERE `profile_version` IS NULL OR `profile_version` = ''
   OR `profile_charset` IS NULL OR `profile_charset` = ''
   OR `target_scope` IS NULL OR `target_scope` = ''
   OR `target_code` IS NULL OR `target_code` = '';
