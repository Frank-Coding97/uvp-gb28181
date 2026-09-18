CREATE TABLE IF NOT EXISTS `gb_device_traffic_hourly` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `stat_hour` datetime(6) NOT NULL,
  `device_code` varchar(64) NOT NULL,
  `channel_code` varchar(64) NOT NULL DEFAULT '',
  `owner_dept_id` bigint unsigned NOT NULL DEFAULT 0,
  `upstream_bytes` bigint unsigned NOT NULL DEFAULT 0,
  `downstream_bytes` bigint unsigned NOT NULL DEFAULT 0,
  `upstream_duration_seconds` bigint NOT NULL DEFAULT 0,
  `downstream_duration_seconds` bigint NOT NULL DEFAULT 0,
  `upstream_sessions` bigint NOT NULL DEFAULT 0,
  `downstream_sessions` bigint NOT NULL DEFAULT 0,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_traffic_hourly_scope` (`stat_hour`,`device_code`,`channel_code`),
  KEY `idx_traffic_hourly_device` (`device_code`,`stat_hour`),
  KEY `idx_traffic_hourly_owner_dept` (`owner_dept_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
