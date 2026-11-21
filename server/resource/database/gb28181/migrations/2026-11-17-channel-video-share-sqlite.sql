CREATE TABLE IF NOT EXISTS gb_channel_video_share (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  channel_pk INTEGER NOT NULL UNIQUE,
  public_id TEXT NOT NULL UNIQUE,
  generation TEXT NOT NULL,
  enabled NUMERIC NOT NULL DEFAULT 0,
  activated_at DATETIME NULL,
  expires_at DATETIME NULL,
  created_by INTEGER NULL,
  device_pk INTEGER NOT NULL,
  device_id TEXT NOT NULL,
  device_epoch INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL,
  policy_actor_id INTEGER NULL,
  policy_revision INTEGER NOT NULL DEFAULT 0,
  key_id TEXT NOT NULL,
  revision INTEGER NOT NULL DEFAULT 1,
  revoked_at DATETIME NULL,
  revoke_reason TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_video_share_enabled_expiry ON gb_channel_video_share (enabled, expires_at);
CREATE INDEX IF NOT EXISTS idx_video_share_created_by ON gb_channel_video_share (created_by);
CREATE INDEX IF NOT EXISTS idx_video_share_device_pk ON gb_channel_video_share (device_pk);
CREATE TABLE IF NOT EXISTS gb_device_video_share_policy (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_pk INTEGER NOT NULL UNIQUE,
  default_enabled NUMERIC NOT NULL DEFAULT 0,
  source TEXT NOT NULL,
  policy_actor_id INTEGER NULL,
  policy_revision INTEGER NOT NULL DEFAULT 0,
  initialized_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS gb_video_share_authority (id INTEGER NOT NULL PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0);
INSERT INTO gb_video_share_authority (id, revision) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;
