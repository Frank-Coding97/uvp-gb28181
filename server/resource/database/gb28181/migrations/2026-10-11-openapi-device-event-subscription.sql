CREATE TABLE IF NOT EXISTS `sys_openapi_device_subscription` (
  `id` varchar(36) NOT NULL,
  `client_id` bigint NOT NULL,
  `device_id` bigint unsigned NOT NULL,
  `kind` varchar(32) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `expires_seconds` int NOT NULL DEFAULT 3600,
  `expires_at` datetime(3) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_openapi_device_subscription` (`client_id`, `device_id`, `kind`),
  KEY `idx_openapi_device_sub_client` (`client_id`),
  KEY `idx_openapi_device_sub_enabled` (`enabled`),
  KEY `idx_openapi_device_sub_expires` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
