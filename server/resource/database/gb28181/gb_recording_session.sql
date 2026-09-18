CREATE TABLE `gb_recording_session` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `channel_id` int unsigned NOT NULL,
  `device_id` varchar(20) NOT NULL,
  `node_id` bigint NOT NULL,
  `vhost` varchar(128) NOT NULL DEFAULT '__defaultVhost__',
  `app` varchar(64) NOT NULL DEFAULT 'rtp',
  `stream` varchar(64) NOT NULL,
  `state` varchar(20) NOT NULL,
  `started_at` datetime DEFAULT NULL,
  `stopped_at` datetime DEFAULT NULL,
  `last_checked_at` datetime DEFAULT NULL,
  `last_error` varchar(500) NOT NULL DEFAULT '',
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_recording_session_media` (`node_id`,`vhost`,`app`,`stream`),
  KEY `idx_recording_session_channel_state` (`channel_id`,`state`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='云端录像会话历史';
