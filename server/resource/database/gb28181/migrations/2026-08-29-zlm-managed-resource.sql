-- Management provenance ledger for ZLM resources (MySQL 5.7+).
-- This table intentionally contains no runtime state, desired state, URL, or credential.
CREATE TABLE IF NOT EXISTS `gb_zlm_managed_resource` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `node_id` bigint NOT NULL,
  `resource_type` varchar(32) NOT NULL,
  `resource_key` varchar(255) NOT NULL,
  `app` varchar(64) NOT NULL DEFAULT '',
  `stream` varchar(255) NOT NULL DEFAULT '',
  `identity_fingerprint` char(64) NOT NULL,
  `summary` varchar(512) NOT NULL DEFAULT '',
  `created_by` bigint unsigned NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `last_observed_at` datetime(3) DEFAULT NULL,
  `tombstoned_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_zlm_managed_resource_identity` (`node_id`,`resource_type`,`resource_key`),
  KEY `idx_gb_zlm_managed_resource_observed` (`node_id`,`last_observed_at`),
  KEY `idx_gb_zlm_managed_resource_tombstone` (`node_id`,`tombstoned_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='ZLM管理资源来源账本';
