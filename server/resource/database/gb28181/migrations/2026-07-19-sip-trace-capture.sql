-- Optional SIP trace diagnostic windows. SIP messages remain in ClickHouse.
CREATE TABLE IF NOT EXISTS `gb_sip_trace_capture` (
  `id` char(36) NOT NULL,
  `device_id` int unsigned NOT NULL,
  `device_code` varchar(20) NOT NULL,
  `created_by` int unsigned NOT NULL,
  `started_at` datetime(3) NOT NULL,
  `planned_end_at` datetime(3) NOT NULL,
  `ended_at` datetime(3) DEFAULT NULL,
  `end_reason` varchar(16) NOT NULL DEFAULT '',
  `active_key` varchar(64) DEFAULT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_sip_trace_capture_active` (`active_key`),
  KEY `idx_sip_trace_capture_device_started` (`device_id`, `started_at`),
  KEY `idx_sip_trace_capture_device_code` (`device_code`),
  KEY `idx_sip_trace_capture_created_by` (`created_by`),
  KEY `idx_sip_trace_capture_planned_end` (`planned_end_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
