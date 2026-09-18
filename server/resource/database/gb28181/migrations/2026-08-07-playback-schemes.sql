-- User-private multi-screen playback schemes. MySQL 5.7+, repeatable.
CREATE TABLE IF NOT EXISTS `gb_playback_scheme` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `owner_user_id` bigint unsigned NOT NULL,
  `owner_dept_id` bigint unsigned NOT NULL,
  `name` varchar(64) NOT NULL,
  `layout_size` smallint NOT NULL,
  `slot_count` int NOT NULL DEFAULT 0,
  `created_by` bigint unsigned NOT NULL,
  `updated_by` bigint unsigned NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_playback_scheme_owner_name` (`owner_user_id`, `name`),
  KEY `idx_playback_scheme_owner_updated` (`owner_user_id`, `updated_at`),
  KEY `idx_playback_scheme_dept` (`owner_dept_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_playback_scheme_slot` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `scheme_id` bigint unsigned NOT NULL,
  `slot_index` int NOT NULL,
  `device_code` varchar(20) NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `device_name_snapshot` varchar(255) NOT NULL,
  `channel_name_snapshot` varchar(255) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_playback_scheme_slot` (`scheme_id`, `slot_index`),
  KEY `idx_playback_scheme_slot_scheme` (`scheme_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
