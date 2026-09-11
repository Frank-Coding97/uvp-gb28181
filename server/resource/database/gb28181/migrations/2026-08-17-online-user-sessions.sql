-- 在线用户会话表(MySQL 5.7+,幂等)。refresh 只保存 SHA-256 摘要,不保存原文。
CREATE TABLE IF NOT EXISTS `sys_user_sessions` (
  `sid` varchar(36) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `refresh_token_hash` char(64) DEFAULT NULL,
  `refresh_jti` varchar(36) DEFAULT NULL,
  `client_ip` varchar(50) NOT NULL DEFAULT '',
  `login_location` varchar(100) NOT NULL DEFAULT '未知',
  `user_agent` varchar(500) NOT NULL DEFAULT '',
  `browser` varchar(100) NOT NULL DEFAULT '未知',
  `os` varchar(100) NOT NULL DEFAULT '未知',
  `login_at` datetime NOT NULL,
  `last_active_at` datetime NOT NULL,
  `session_expires_at` datetime NOT NULL,
  `revoked_at` datetime DEFAULT NULL,
  `revoke_reason` varchar(32) DEFAULT NULL,
  `revoked_by` bigint unsigned DEFAULT NULL,
  `created_at` datetime DEFAULT NULL,
  `updated_at` datetime DEFAULT NULL,
  PRIMARY KEY (`sid`),
  KEY `idx_user_id` (`user_id`),
  KEY `idx_session_valid` (`revoked_at`,`session_expires_at`,`login_at`),
  KEY `idx_client_ip` (`client_ip`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='后台登录会话';
