-- UVP 国标公网安全防护一期安全聚合/封禁/策略/审计 (MySQL)
CREATE TABLE IF NOT EXISTS `gb_sip_security_event` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `bucket_at` DATETIME NOT NULL,
  `source_ip` VARCHAR(64) NOT NULL,
  `address_family` VARCHAR(8) NOT NULL,
  `transport` VARCHAR(8) NOT NULL,
  `method` VARCHAR(16) NOT NULL,
  `reason` VARCHAR(32) NOT NULL,
  `action` VARCHAR(16) NOT NULL,
  `count` BIGINT NOT NULL DEFAULT 0,
  `score_delta` BIGINT NOT NULL DEFAULT 0,
  `first_seen_at` DATETIME NOT NULL,
  `last_seen_at` DATETIME NOT NULL,
  `sample_event_id` VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_sip_security_event` (`bucket_at`,`source_ip`,`transport`,`method`,`reason`,`action`),
  KEY `idx_gb_sip_security_event_source_time` (`source_ip`,`last_seen_at`),
  KEY `idx_gb_sip_security_event_reason_time` (`reason`,`last_seen_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `gb_sip_security_ban` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `source_ip` VARCHAR(64) NOT NULL,
  `address_family` VARCHAR(8) NOT NULL,
  `status` VARCHAR(16) NOT NULL,
  `reason` VARCHAR(32) NOT NULL,
  `rule_id` VARCHAR(64) NOT NULL,
  `score` INT NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL,
  `expires_at` DATETIME NOT NULL,
  `unbanned_at` DATETIME NULL,
  `unbanned_by` VARCHAR(64) NOT NULL DEFAULT '',
  `origin` VARCHAR(16) NOT NULL,
  `agent_state` VARCHAR(16) NOT NULL,
  `decision_id` VARCHAR(64) NOT NULL,
  `last_error` VARCHAR(512) NOT NULL DEFAULT '',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_sip_security_ban_decision` (`decision_id`),
  KEY `idx_gb_sip_security_ban_source_status` (`source_ip`,`status`),
  KEY `idx_gb_sip_security_ban_expiry` (`expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `gb_sip_security_policy` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `scope_key` VARCHAR(32) NOT NULL,
  `mode` VARCHAR(16) NOT NULL,
  `window_seconds` INT NOT NULL,
  `ban_score` INT NOT NULL,
  `max_packet_bytes` INT NOT NULL,
  `max_udp_per_window` INT NOT NULL,
  `max_tcp_connections` INT NOT NULL,
  `sample_per_source` INT NOT NULL,
  `nonce_ttl_seconds` INT NOT NULL,
  `ban_ttl_steps` VARCHAR(1024) NOT NULL,
  `allowlist_text` VARCHAR(4096) NOT NULL,
  `updated_by` BIGINT NOT NULL DEFAULT 0,
  `updated_at` DATETIME NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_sip_security_policy_scope` (`scope_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `gb_sip_security_audit` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `actor` VARCHAR(64) NOT NULL,
  `action` VARCHAR(32) NOT NULL,
  `target` VARCHAR(128) NOT NULL,
  `reason` VARCHAR(255) NOT NULL,
  `decision_id` VARCHAR(64) NOT NULL DEFAULT '',
  `created_at` DATETIME NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_gb_sip_security_audit_time` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO `gb_sip_security_policy` (`scope_key`,`mode`,`window_seconds`,`ban_score`,`max_packet_bytes`,`max_udp_per_window`,`max_tcp_connections`,`sample_per_source`,`nonce_ttl_seconds`,`ban_ttl_steps`,`allowlist_text`,`updated_at`)
SELECT 'global','observe',60,100,65536,120,32,3,60,'100:600;250:3600;500:86400','',NOW()
WHERE NOT EXISTS (SELECT 1 FROM `gb_sip_security_policy` WHERE `scope_key` = 'global');
