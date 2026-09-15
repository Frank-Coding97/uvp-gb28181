-- Cloud recording plan domain tables (MySQL 5.7+).
SET @recording_mode_ddl = IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'recording_mode') = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `recording_mode` varchar(16) NOT NULL DEFAULT ''off'' COMMENT ''录像模式 off/continuous/scheduled'' AFTER `audio_enabled`',
  'SELECT 1'
);
PREPARE recording_mode_stmt FROM @recording_mode_ddl;
EXECUTE recording_mode_stmt;
DEALLOCATE PREPARE recording_mode_stmt;
UPDATE `gb_channel` SET `recording_mode` = CASE WHEN `cloud_recording_enabled` = 1 THEN 'continuous' ELSE 'off' END;

CREATE TABLE IF NOT EXISTS `gb_recording_plan` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `name` varchar(128) NOT NULL,
  `description` varchar(500) NOT NULL DEFAULT '', `status` tinyint NOT NULL DEFAULT 1,
  `version` bigint unsigned NOT NULL DEFAULT 1, `owner_dept_id` int unsigned NOT NULL,
  `created_by` int unsigned NOT NULL DEFAULT 0, `updated_by` int unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL, `deleted_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`), KEY `idx_recording_plan_dept_deleted` (`owner_dept_id`,`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划';

CREATE TABLE IF NOT EXISTS `gb_recording_plan_period` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `plan_id` bigint unsigned NOT NULL,
  `weekday` tinyint NOT NULL, `start_slot` smallint NOT NULL, `end_slot` smallint NOT NULL,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`), KEY `idx_recording_plan_period_plan_weekday` (`plan_id`,`weekday`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划每周半小时时段';

CREATE TABLE IF NOT EXISTS `gb_recording_plan_binding` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `plan_id` bigint unsigned NOT NULL,
  `channel_id` int unsigned NOT NULL, `owner_dept_id` int unsigned NOT NULL,
  `assigned_by` int unsigned NOT NULL DEFAULT 0, `assigned_at` datetime(3) NOT NULL,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_recording_plan_binding_channel` (`channel_id`),
  KEY `idx_recording_plan_binding_plan` (`plan_id`), KEY `idx_recording_plan_binding_dept` (`owner_dept_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划通道绑定';

CREATE TABLE IF NOT EXISTS `gb_recording_plan_channel_state` (
  `channel_id` int unsigned NOT NULL, `plan_id` bigint unsigned DEFAULT NULL, `plan_version` bigint unsigned NOT NULL DEFAULT 0,
  `desired_state` varchar(24) NOT NULL, `actual_state` varchar(32) NOT NULL,
  `reason_code` varchar(64) NOT NULL DEFAULT '', `reason_message` varchar(500) NOT NULL DEFAULT '',
  `next_transition_at` datetime(3) DEFAULT NULL, `next_retry_at` datetime(3) DEFAULT NULL,
  `reconcile_at` datetime(3) NOT NULL, `attempt_count` int NOT NULL DEFAULT 0,
  `generation` bigint unsigned NOT NULL DEFAULT 0, `stream_id` varchar(64) NOT NULL DEFAULT '',
  `recording_session_id` bigint unsigned DEFAULT NULL, `node_id` varchar(64) NOT NULL DEFAULT '',
  `last_media_at` datetime(3) DEFAULT NULL, `last_success_at` datetime(3) DEFAULT NULL,
  `lease_owner` varchar(128) NOT NULL DEFAULT '', `lease_until` datetime(3) DEFAULT NULL,
  `state_version` bigint unsigned NOT NULL DEFAULT 0, `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`channel_id`), KEY `idx_recording_plan_state_reconcile` (`reconcile_at`,`channel_id`),
  KEY `idx_recording_plan_state_retry` (`next_retry_at`,`channel_id`), KEY `idx_recording_plan_state_plan_actual` (`plan_id`,`actual_state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划通道执行状态';

CREATE TABLE IF NOT EXISTS `gb_recording_plan_execution` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `plan_id` bigint unsigned DEFAULT NULL, `channel_id` int unsigned NOT NULL,
  `device_id` varchar(20) NOT NULL DEFAULT '', `action` varchar(32) NOT NULL, `trigger_source` varchar(32) NOT NULL,
  `stage` varchar(32) NOT NULL DEFAULT '', `attempt` int NOT NULL DEFAULT 1, `result` varchar(24) NOT NULL,
  `reason_code` varchar(64) NOT NULL DEFAULT '', `reason_message` varchar(500) NOT NULL DEFAULT '',
  `stream_id` varchar(64) NOT NULL DEFAULT '', `node_id` varchar(64) NOT NULL DEFAULT '',
  `recording_session_id` bigint unsigned DEFAULT NULL, `generation` bigint unsigned NOT NULL DEFAULT 0,
  `started_at` datetime(3) NOT NULL, `ended_at` datetime(3) DEFAULT NULL, `duration_ms` bigint NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL, PRIMARY KEY (`id`),
  KEY `idx_recording_plan_execution_channel` (`channel_id`,`started_at`), KEY `idx_recording_plan_execution_plan` (`plan_id`,`started_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划执行流水';

CREATE TABLE IF NOT EXISTS `gb_recording_plan_gap` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `plan_id` bigint unsigned DEFAULT NULL, `channel_id` int unsigned NOT NULL,
  `started_at` datetime(3) NOT NULL, `ended_at` datetime(3) DEFAULT NULL, `duration_ms` bigint NOT NULL DEFAULT 0,
  `reason_code` varchar(64) NOT NULL, `reason_message` varchar(500) NOT NULL DEFAULT '',
  `recovered` tinyint(1) NOT NULL DEFAULT 0, `execution_id` bigint unsigned DEFAULT NULL,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL, PRIMARY KEY (`id`),
  KEY `idx_recording_plan_gap_channel` (`channel_id`,`started_at`), KEY `idx_recording_plan_gap_plan` (`plan_id`,`started_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='录像计划缺口';
