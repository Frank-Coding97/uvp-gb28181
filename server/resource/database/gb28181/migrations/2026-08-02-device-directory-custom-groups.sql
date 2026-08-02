-- Device custom groups. MySQL 5.7+, repeatable.
CREATE TABLE IF NOT EXISTS `gb_custom_group` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `owner_dept_id` bigint unsigned NOT NULL,
  `parent_id` bigint unsigned NOT NULL DEFAULT 0,
  `path` varchar(1024) NOT NULL,
  `depth` tinyint unsigned NOT NULL DEFAULT 0,
  `name` varchar(64) NOT NULL,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_custom_group_sibling_name` (`owner_dept_id`,`parent_id`,`name`),
  KEY `idx_custom_group_parent` (`parent_id`),
  KEY `idx_custom_group_dept_path` (`owner_dept_id`,`path`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `gb_custom_group_device` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `group_id` bigint unsigned NOT NULL,
  `device_id` bigint unsigned NOT NULL,
  `created_by` bigint unsigned NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_custom_group_device` (`group_id`,`device_id`),
  KEY `idx_custom_group_device_group` (`group_id`),
  KEY `idx_custom_group_device_device` (`device_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
