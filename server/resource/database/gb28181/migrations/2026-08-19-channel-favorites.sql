CREATE TABLE IF NOT EXISTS `gb_channel_favorite_group` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `owner_user_id` bigint unsigned NOT NULL,
  `name` varchar(64) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_channel_favorite_group_owner_name` (`owner_user_id`,`name`),
  KEY `idx_gb_channel_favorite_group_owner` (`owner_user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `gb_channel_favorite_item` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `group_id` bigint unsigned NOT NULL,
  `device_code` varchar(64) NOT NULL,
  `channel_code` varchar(64) NOT NULL,
  `device_name` varchar(255) NOT NULL DEFAULT '',
  `channel_name` varchar(255) NOT NULL DEFAULT '',
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_channel_favorite_item_code` (`group_id`,`device_code`,`channel_code`),
  KEY `idx_gb_channel_favorite_item_group` (`group_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
