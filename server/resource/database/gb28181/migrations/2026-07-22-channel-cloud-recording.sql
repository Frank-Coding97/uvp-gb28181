ALTER TABLE `gb_channel`
  ADD COLUMN `cloud_recording_enabled` tinyint(1) NOT NULL DEFAULT '0' COMMENT '云端录像期望开关' AFTER `on_demand_live`,
  ADD COLUMN `cloud_recording_state` varchar(20) NOT NULL DEFAULT 'disabled' COMMENT '云端录像运行状态' AFTER `cloud_recording_enabled`,
  ADD COLUMN `cloud_recording_error` varchar(500) NOT NULL DEFAULT '' COMMENT '云端录像最近错误' AFTER `cloud_recording_state`,
  ADD COLUMN `cloud_recording_updated_at` datetime DEFAULT NULL COMMENT '云端录像状态更新时间' AFTER `cloud_recording_error`;

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

CREATE TABLE `gb_recording_file` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `session_id` bigint unsigned DEFAULT NULL,
  `channel_id` int unsigned NOT NULL,
  `device_id` varchar(20) NOT NULL,
  `node_id` bigint NOT NULL,
  `vhost` varchar(128) NOT NULL,
  `app` varchar(64) NOT NULL,
  `stream` varchar(64) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `file_path` varchar(1000) NOT NULL,
  `folder` varchar(1000) NOT NULL DEFAULT '',
  `url` varchar(1000) NOT NULL DEFAULT '',
  `start_time` datetime NOT NULL,
  `time_len` decimal(12,3) NOT NULL DEFAULT '0.000',
  `file_size` bigint unsigned NOT NULL DEFAULT '0',
  `created_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_recording_file_node_path` (`node_id`,`file_path`(766)),
  KEY `idx_recording_file_channel_start` (`channel_id`,`start_time`),
  KEY `idx_recording_file_device_start` (`device_id`,`start_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='云端录像文件索引';
