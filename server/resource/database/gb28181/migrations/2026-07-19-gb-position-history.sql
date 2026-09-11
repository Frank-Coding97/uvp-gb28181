-- 2026-07-19: append-only history for accepted GB28181 MobilePosition notifications.
CREATE TABLE IF NOT EXISTS `gb_mobile_position_history` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` int unsigned NOT NULL,
  `source_code` varchar(20) NOT NULL,
  `channel_id` int unsigned DEFAULT NULL,
  `event_time` datetime NOT NULL,
  `received_at` datetime NOT NULL,
  `longitude` decimal(10,6) NOT NULL,
  `latitude` decimal(10,6) NOT NULL,
  `speed` double DEFAULT NULL,
  `direction` double DEFAULT NULL,
  `altitude` double DEFAULT NULL,
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_position_history_device_source_time` (`device_id`, `source_code`, `event_time`),
  KEY `idx_position_history_channel_time` (`channel_id`, `event_time`),
  KEY `idx_position_history_received_at` (`received_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
