CREATE TABLE IF NOT EXISTS gb_channel_video_share (
  id BIGSERIAL PRIMARY KEY,
  channel_pk BIGINT NOT NULL UNIQUE,
  public_id VARCHAR(64) NOT NULL UNIQUE,
  generation VARCHAR(64) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  activated_at TIMESTAMP NULL,
  expires_at TIMESTAMP NULL,
  created_by BIGINT NULL,
  device_pk BIGINT NOT NULL,
  device_id VARCHAR(20) NOT NULL,
  device_epoch BIGINT NOT NULL DEFAULT 0,
  source VARCHAR(16) NOT NULL,
  policy_actor_id BIGINT NULL,
  policy_revision BIGINT NOT NULL DEFAULT 0,
  key_id VARCHAR(64) NOT NULL,
  revision BIGINT NOT NULL DEFAULT 1,
  revoked_at TIMESTAMP NULL,
  revoke_reason VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_video_share_enabled_expiry ON gb_channel_video_share (enabled, expires_at);
CREATE INDEX IF NOT EXISTS idx_video_share_created_by ON gb_channel_video_share (created_by);
CREATE INDEX IF NOT EXISTS idx_video_share_device_pk ON gb_channel_video_share (device_pk);
CREATE TABLE IF NOT EXISTS gb_device_video_share_policy (
  id BIGSERIAL PRIMARY KEY,
  device_pk BIGINT NOT NULL UNIQUE,
  default_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  source VARCHAR(16) NOT NULL,
  policy_actor_id BIGINT NULL,
  policy_revision BIGINT NOT NULL DEFAULT 0,
  initialized_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS gb_video_share_authority (id BIGINT NOT NULL PRIMARY KEY, revision BIGINT NOT NULL DEFAULT 0);
INSERT INTO gb_video_share_authority (id, revision) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;
