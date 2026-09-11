-- User-private multi-screen playback schemes. PostgreSQL 12+, repeatable.
CREATE TABLE IF NOT EXISTS gb_playback_scheme (
  id BIGSERIAL PRIMARY KEY,
  owner_user_id BIGINT NOT NULL,
  owner_dept_id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL,
  layout_size SMALLINT NOT NULL,
  slot_count INTEGER NOT NULL DEFAULT 0,
  created_by BIGINT NOT NULL,
  updated_by BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_playback_scheme_owner_name UNIQUE (owner_user_id, name)
);
CREATE INDEX IF NOT EXISTS idx_playback_scheme_owner_updated ON gb_playback_scheme (owner_user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_playback_scheme_dept ON gb_playback_scheme (owner_dept_id);

CREATE TABLE IF NOT EXISTS gb_playback_scheme_slot (
  id BIGSERIAL PRIMARY KEY,
  scheme_id BIGINT NOT NULL,
  slot_index INTEGER NOT NULL,
  device_code VARCHAR(20) NOT NULL,
  channel_code VARCHAR(20) NOT NULL,
  device_name_snapshot VARCHAR(255) NOT NULL,
  channel_name_snapshot VARCHAR(255) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_playback_scheme_slot UNIQUE (scheme_id, slot_index)
);
CREATE INDEX IF NOT EXISTS idx_playback_scheme_slot_scheme ON gb_playback_scheme_slot (scheme_id);
