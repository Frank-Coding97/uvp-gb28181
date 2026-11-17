CREATE TABLE IF NOT EXISTS gb_channel_video_share (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  channel_pk BIGINT UNSIGNED NOT NULL,
  public_id VARCHAR(64) NOT NULL,
  generation VARCHAR(64) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  activated_at DATETIME(3) NULL,
  expires_at DATETIME(3) NULL,
  created_by BIGINT UNSIGNED NULL,
  device_pk BIGINT UNSIGNED NOT NULL,
  device_id VARCHAR(20) NOT NULL,
  device_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
  source VARCHAR(16) NOT NULL,
  policy_actor_id BIGINT UNSIGNED NULL,
  policy_revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
  key_id VARCHAR(64) NOT NULL,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
  revoked_at DATETIME(3) NULL,
  revoke_reason VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_video_share_channel (channel_pk),
  UNIQUE KEY uk_video_share_public_id (public_id),
  KEY idx_video_share_enabled_expiry (enabled, expires_at),
  KEY idx_video_share_created_by (created_by),
  KEY idx_video_share_device_pk (device_pk)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gb_device_video_share_policy (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  device_pk BIGINT UNSIGNED NOT NULL,
  default_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  source VARCHAR(16) NOT NULL,
  policy_actor_id BIGINT UNSIGNED NULL,
  policy_revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
  initialized_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY uk_video_share_device_policy (device_pk)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS gb_video_share_authority (
  id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO gb_video_share_authority (id, revision) VALUES (1, 0);
