-- 2026-07-19: GB28181 full PTZ backend tables. Statements are idempotent.
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
  `error_code` varchar(64) DEFAULT NULL,
  `error_message` text,
  `actor_id` bigint unsigned NOT NULL DEFAULT 0,
  `actor_dept_id` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `sent_at` datetime(3) DEFAULT NULL,
  `completed_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_ptz_operation_id` (`operation_id`),
  UNIQUE KEY `uk_ptz_operation_idempotency` (`channel_id`,`idempotency_key`),
  KEY `idx_ptz_operation_channel_time` (`channel_id`,`created_at`),
  KEY `idx_ptz_operation_device_sn` (`device_id`,`sn`),
  KEY `idx_ptz_operation_status_time` (`status`,`created_at`), KEY `idx_ptz_operation_call_id` (`call_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_state` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL, `channel_code` varchar(20) NOT NULL,
  `pan` decimal(18,6) DEFAULT NULL, `tilt` decimal(18,6) DEFAULT NULL, `zoom` decimal(18,6) DEFAULT NULL,
  `focus` decimal(18,6) DEFAULT NULL, `iris` decimal(18,6) DEFAULT NULL,
  `home_enabled` tinyint(1) DEFAULT NULL, `home_pan` decimal(18,6) DEFAULT NULL,
  `home_tilt` decimal(18,6) DEFAULT NULL, `home_zoom` decimal(18,6) DEFAULT NULL,
  `device_time` datetime(3) DEFAULT NULL, `received_at` datetime(3) NOT NULL,
  `source_sn` int NOT NULL DEFAULT 0, `freshness` varchar(16) NOT NULL DEFAULT 'unknown',
  `dedupe_key` varchar(128) DEFAULT NULL, `raw_summary` text,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_ptz_state_channel` (`channel_id`),
  KEY `idx_ptz_state_device` (`device_id`), KEY `idx_ptz_state_received` (`received_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_preset` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL, `preset_id` int NOT NULL,
  `name` varchar(255) DEFAULT NULL, `status` varchar(16) NOT NULL DEFAULT 'unknown',
  `last_operation_id` varchar(64) DEFAULT NULL, `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_ptz_preset_channel_number` (`channel_id`,`preset_id`), KEY `idx_ptz_preset_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_ptz_cruise_track` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT, `device_id` bigint unsigned NOT NULL,
  `channel_id` bigint unsigned NOT NULL, `track_id` int NOT NULL,
  `name` varchar(255) DEFAULT NULL, `enabled` tinyint(1) DEFAULT NULL,
  `detail_json` text, `raw_summary` text, `device_time` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) NOT NULL, `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`), UNIQUE KEY `uk_ptz_cruise_channel_track` (`channel_id`,`track_id`), KEY `idx_ptz_cruise_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
