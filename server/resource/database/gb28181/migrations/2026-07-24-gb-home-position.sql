-- 2026-07-24: GB28181 home-position response lifecycle (MySQL 5.7+).
-- Idempotent upgrade: legacy gb_ptz_state.home_* columns are intentionally retained.

CREATE TABLE IF NOT EXISTS `gb_ptz_operation` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `operation_id` varchar(64) NOT NULL,
  `idempotency_key` varchar(128) NOT NULL,
  `device_id` bigint unsigned NOT NULL,
  `device_code` varchar(20) NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `cmd_type` varchar(64) NOT NULL,
  `action` varchar(64) DEFAULT NULL,
  `payload_json` text,
  `sn` int NOT NULL,
  `call_id` varchar(255) DEFAULT NULL,
  `cseq` varchar(64) DEFAULT NULL,
  `sip_status` int NOT NULL DEFAULT 0,
  `device_result` varchar(32) DEFAULT NULL,
  `device_error` text,
  `status` varchar(16) NOT NULL,
  `attempt` int NOT NULL DEFAULT 1,
  `response_required` tinyint(1) NOT NULL DEFAULT 0,
  `max_attempts` int NOT NULL DEFAULT 1,
  `error_code` varchar(64) DEFAULT NULL,
  `error_message` text,
  `actor_id` bigint unsigned NOT NULL DEFAULT 0,
  `actor_dept_id` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `sent_at` datetime(3) DEFAULT NULL,
  `completed_at` datetime(3) DEFAULT NULL,
  `queue_deadline_at` datetime(3) DEFAULT NULL,
  `dispatch_started_at` datetime(3) DEFAULT NULL,
  `transport_deadline_at` datetime(3) DEFAULT NULL,
  `deadline_at` datetime(3) DEFAULT NULL,
  `next_attempt_at` datetime(3) DEFAULT NULL,
  `response_call_id` varchar(255) DEFAULT NULL,
  `response_cseq` varchar(64) DEFAULT NULL,
  `response_at` datetime(3) DEFAULT NULL,
  `response_has_data` tinyint(1) DEFAULT NULL,
  `trigger_operation_id` varchar(64) DEFAULT NULL,
  `reconcile_operation_id` varchar(64) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_operation_id` (`operation_id`),
  UNIQUE KEY `uk_ptz_operation_idempotency` (`channel_id`,`idempotency_key`),
  KEY `idx_ptz_operation_channel_time` (`channel_id`,`created_at`),
  KEY `idx_ptz_operation_channel_cmd_id` (`channel_id`,`cmd_type`,`id`),
  KEY `idx_ptz_operation_device_sn` (`device_id`,`sn`),
  KEY `idx_ptz_operation_status_time` (`status`,`created_at`),
  KEY `idx_ptz_operation_status_next_attempt` (`status`,`next_attempt_at`),
  KEY `idx_ptz_operation_status_queue_deadline` (`status`,`queue_deadline_at`),
  KEY `idx_ptz_operation_status_transport_deadline` (`status`,`transport_deadline_at`),
  KEY `idx_ptz_operation_status_deadline` (`status`,`deadline_at`),
  KEY `idx_ptz_operation_call_id` (`call_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_state` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `pan` decimal(18,6) DEFAULT NULL,
  `tilt` decimal(18,6) DEFAULT NULL,
  `zoom` decimal(18,6) DEFAULT NULL,
  `focus` decimal(18,6) DEFAULT NULL,
  `iris` decimal(18,6) DEFAULT NULL,
  `device_time` datetime(3) DEFAULT NULL,
  `received_at` datetime(3) NOT NULL,
  `source_sn` int NOT NULL DEFAULT 0,
  `freshness` varchar(16) NOT NULL DEFAULT 'unknown',
  `dedupe_key` varchar(128) DEFAULT NULL,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_state_channel` (`channel_id`),
  KEY `idx_ptz_state_device` (`device_id`),
  KEY `idx_ptz_state_received` (`received_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_preset` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `preset_id` int NOT NULL,
  `name` varchar(255) DEFAULT NULL,
  `status` varchar(16) NOT NULL DEFAULT 'unknown',
  `last_operation_id` varchar(64) DEFAULT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_preset_channel_number` (`channel_id`,`preset_id`),
  KEY `idx_ptz_preset_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_cruise_track` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `track_id` int NOT NULL,
  `name` varchar(255) DEFAULT NULL,
  `enabled` tinyint(1) DEFAULT NULL,
  `detail_json` text,
  `raw_summary` text,
  `device_time` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_cruise_channel_track` (`channel_id`,`track_id`),
  KEY `idx_ptz_cruise_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_operation_attempt` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `operation_id` bigint unsigned NOT NULL,
  `attempt_no` int NOT NULL,
  `sn` int NOT NULL,
  `status` varchar(16) NOT NULL,
  `call_id` varchar(255) DEFAULT NULL,
  `cseq` varchar(64) DEFAULT NULL,
  `sip_status` int NOT NULL DEFAULT 0,
  `started_at` datetime(3) NOT NULL,
  `lease_until` datetime(3) NOT NULL,
  `sent_at` datetime(3) DEFAULT NULL,
  `completed_at` datetime(3) DEFAULT NULL,
  `error_code` varchar(64) DEFAULT NULL,
  `error_message` text,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_operation_attempt` (`operation_id`,`attempt_no`),
  KEY `idx_ptz_attempt_status_lease` (`status`,`lease_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_home_position` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `enabled` tinyint(1) NOT NULL,
  `reset_time` int DEFAULT NULL,
  `preset_id` int DEFAULT NULL,
  `enabled_encoding` varchar(32) NOT NULL DEFAULT 'numeric',
  `confirmed_at` datetime(3) NOT NULL,
  `source` varchar(32) NOT NULL,
  `verification` varchar(16) NOT NULL,
  `source_sn` int NOT NULL DEFAULT 0,
  `source_operation_id` varchar(64) DEFAULT NULL,
  `source_operation_seq` bigint unsigned NOT NULL DEFAULT 0,
  `raw_summary` text,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_ptz_home_position_channel` (`channel_id`),
  KEY `idx_ptz_home_position_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

SET @schema_name := DATABASE();

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'response_required') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `response_required` tinyint(1) NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'max_attempts') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `max_attempts` int NOT NULL DEFAULT 1', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'queue_deadline_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `queue_deadline_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'dispatch_started_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `dispatch_started_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'transport_deadline_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `transport_deadline_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'deadline_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `deadline_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'next_attempt_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `next_attempt_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'response_call_id') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `response_call_id` varchar(255) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'response_cseq') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `response_cseq` varchar(64) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'response_at') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `response_at` datetime(3) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'response_has_data') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `response_has_data` tinyint(1) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'trigger_operation_id') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `trigger_operation_id` varchar(64) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND column_name = 'reconcile_operation_id') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD COLUMN `reconcile_operation_id` varchar(64) DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_channel_cmd_id') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_channel_cmd_id` (`channel_id`,`cmd_type`,`id`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_status_next_attempt') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_status_next_attempt` (`status`,`next_attempt_at`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_status_queue_deadline') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_status_queue_deadline` (`status`,`queue_deadline_at`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_status_transport_deadline') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_status_transport_deadline` (`status`,`transport_deadline_at`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_ptz_operation' AND index_name = 'idx_ptz_operation_status_deadline') = 0,
  'ALTER TABLE `gb_ptz_operation` ADD INDEX `idx_ptz_operation_status_deadline` (`status`,`deadline_at`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

UPDATE `gb_ptz_operation`
SET `status` = 'unknown',
    `error_code` = 'TRANSPORT_UNKNOWN',
    `error_message` = COALESCE(NULLIF(`error_message`, ''), 'legacy home-position operation upgraded without response metadata'),
    `completed_at` = COALESCE(`completed_at`, CURRENT_TIMESTAMP(3))
WHERE `action` IN ('home_position', 'refresh_home_position')
  AND `status` IN ('queued', 'sent')
  AND `response_required` = 0
  AND `completed_at` IS NULL;
