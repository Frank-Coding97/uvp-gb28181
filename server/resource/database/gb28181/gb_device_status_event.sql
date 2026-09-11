-- GB28181 device lifecycle event history.
DROP TABLE IF EXISTS `gb_device_status_event`;
CREATE TABLE `gb_device_status_event` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` int unsigned NOT NULL COMMENT 'gb_device.id',
  `device_code` varchar(20) NOT NULL COMMENT 'device code snapshot',
  `event_type` varchar(32) NOT NULL COMMENT 'register_online/unregister_offline/heartbeat_timeout/heartbeat_recovered/register_renewed',
  `from_status` tinyint DEFAULT NULL COMMENT 'previous online status',
  `to_status` tinyint NOT NULL COMMENT 'resulting online status',
  `occurred_at` datetime NOT NULL COMMENT 'business event time',
  `source` varchar(32) NOT NULL COMMENT 'register/unregister/keepalive/offline_scanner',
  `register_expires` int DEFAULT NULL,
  `keepalive_interval` int DEFAULT NULL,
  `ip` varchar(64) NOT NULL DEFAULT '',
  `port` int NOT NULL DEFAULT 0,
  `transport` varchar(8) NOT NULL DEFAULT '',
  `detail` text DEFAULT NULL,
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_device_occurred` (`device_id`, `occurred_at`, `id`),
  KEY `idx_event_type_occurred` (`event_type`, `occurred_at`),
  KEY `idx_device_code` (`device_code`),
  KEY `idx_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='GB28181 device status event history';
