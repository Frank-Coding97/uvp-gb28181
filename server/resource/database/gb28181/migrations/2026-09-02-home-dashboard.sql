-- Durable home-dashboard layout and metric facts. MySQL 5.7+, repeatable.
CREATE TABLE IF NOT EXISTS `gb_dashboard_layout` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id` bigint unsigned NOT NULL,
  `dashboard_key` varchar(32) NOT NULL,
  `schema_version` int NOT NULL,
  `revision` bigint unsigned NOT NULL DEFAULT 1,
  `layout_json` longtext NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_dashboard_layout_user_key` (`user_id`,`dashboard_key`),
  KEY `idx_dashboard_layout_updated` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_sip_metric_minute` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `bucket_start` datetime(3) NOT NULL,
  `method` varchar(16) NOT NULL,
  `direction` varchar(8) NOT NULL,
  `request_count` bigint unsigned NOT NULL DEFAULT 0,
  `transaction_count` bigint unsigned NOT NULL DEFAULT 0,
  `transaction_success` bigint unsigned NOT NULL DEFAULT 0,
  `transaction_failure` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_sip_metric_minute_bucket` (`bucket_start`,`method`,`direction`),
  KEY `idx_sip_metric_minute_bucket` (`bucket_start`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_sip_metric_flush` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `flush_id` varchar(64) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_sip_metric_flush_id` (`flush_id`),
  KEY `idx_sip_metric_flush_created` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_sip_metric_gap` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `started_at` datetime(3) NOT NULL,
  `ended_at` datetime(3) NOT NULL,
  `reason` varchar(32) NOT NULL,
  `dropped_count` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_sip_metric_gap_window` (`started_at`,`ended_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_play_attempt` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `correlation_id` varchar(64) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `device_code` varchar(20) NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `node_id` bigint NOT NULL DEFAULT 0,
  `reused` tinyint(1) NOT NULL DEFAULT 0,
  `outcome` varchar(32) NOT NULL,
  `failure_stage` varchar(32) NOT NULL DEFAULT '',
  `started_at` datetime(3) NOT NULL,
  `finished_at` datetime(3) NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_play_attempt_correlation` (`correlation_id`),
  KEY `idx_play_attempt_user_started` (`user_id`,`started_at`),
  KEY `idx_play_attempt_device_started` (`device_code`,`started_at`),
  KEY `idx_play_attempt_outcome_started` (`outcome`,`started_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
