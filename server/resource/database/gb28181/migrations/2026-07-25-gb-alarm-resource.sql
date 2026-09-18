-- 2026-07-25: persist GB/T 28181 alarm input/output Catalog resources.
-- MySQL 5.7+; repeatable migration.

SET @schema_name := DATABASE();

CREATE TABLE IF NOT EXISTS `gb_alarm_resource` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `owner_dept_id` bigint unsigned NOT NULL,
  `device_id` bigint unsigned NOT NULL DEFAULT 0,
  `device_code` varchar(20) NOT NULL,
  `alarm_code` varchar(20) NOT NULL,
  `resource_type` varchar(16) NOT NULL,
  `type_code` varchar(3) NOT NULL,
  `name` varchar(255) NOT NULL,
  `raw_parent_ids` varchar(512) NOT NULL DEFAULT '',
  `status` tinyint NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  `deleted_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alarm_resource_code` (`owner_dept_id`, `device_code`, `alarm_code`),
  KEY `idx_alarm_resource_device` (`owner_dept_id`, `device_code`),
  KEY `idx_alarm_resource_device_id` (`device_id`),
  KEY `idx_alarm_resource_alarm_code` (`alarm_code`),
  KEY `idx_alarm_resource_type` (`resource_type`),
  KEY `idx_alarm_resource_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_alarm_resource_parent` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `alarm_resource_id` bigint unsigned NOT NULL,
  `parent_code` varchar(20) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alarm_resource_parent` (`alarm_resource_id`, `parent_code`),
  KEY `idx_alarm_parent_resource` (`alarm_resource_id`),
  KEY `idx_alarm_parent_code` (`parent_code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `gb_alarm_binding` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `device_id` bigint unsigned NOT NULL,
  `channel_code` varchar(20) NOT NULL,
  `alarm_resource_id` bigint unsigned NOT NULL,
  `source` varchar(16) NOT NULL DEFAULT 'manual',
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_alarm_binding_channel` (`device_id`, `channel_code`),
  KEY `idx_alarm_binding_device` (`device_id`),
  KEY `idx_alarm_binding_resource` (`alarm_resource_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node') = 1
  AND (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND column_name = 'alarm_resource_id') = 0,
  'ALTER TABLE `gb_catalog_node` ADD COLUMN `alarm_resource_id` bigint unsigned DEFAULT NULL', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @sql := IF((SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node') = 1
  AND (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = @schema_name AND table_name = 'gb_catalog_node' AND index_name = 'idx_catalog_alarm_resource') = 0,
  'ALTER TABLE `gb_catalog_node` ADD INDEX `idx_catalog_alarm_resource` (`alarm_resource_id`)', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
