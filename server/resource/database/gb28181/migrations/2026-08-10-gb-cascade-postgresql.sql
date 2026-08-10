-- GB28181 cascade tables. PostgreSQL 12+, repeatable for a new cascade installation.
CREATE TABLE IF NOT EXISTS gb_cascade_platform (
  id BIGSERIAL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  upstream_server_id VARCHAR(20) NOT NULL,
  upstream_domain VARCHAR(255) NOT NULL,
  host VARCHAR(255) NOT NULL,
  port INTEGER NOT NULL,
  local_device_id VARCHAR(20) NOT NULL,
  local_domain VARCHAR(255) NOT NULL,
  local_sip_ip VARCHAR(45) NOT NULL,
  local_sip_port INTEGER NOT NULL,
  media_advertise_ip VARCHAR(45), auth_username VARCHAR(255), secret_nonce BYTEA,
  secret_ciphertext BYTEA, secret_alg VARCHAR(32), secret_key_version VARCHAR(64),
  profile_override VARCHAR(16) NOT NULL DEFAULT 'auto', reported_gb_version VARCHAR(16),
  reported_gb_version_at TIMESTAMP(3), effective_version VARCHAR(16) NOT NULL DEFAULT '2016',
  effective_version_source VARCHAR(32) NOT NULL DEFAULT 'default', effective_version_at TIMESTAMP(3), charset_override VARCHAR(32),
  register_expires INTEGER NOT NULL DEFAULT 3600, keepalive_interval INTEGER NOT NULL DEFAULT 60,
  retry_policy TEXT, transport VARCHAR(16) NOT NULL DEFAULT 'UDP', catalog_batch_size INTEGER NOT NULL DEFAULT 100,
  publish_platform BOOLEAN NOT NULL DEFAULT FALSE, publish_civil BOOLEAN NOT NULL DEFAULT FALSE,
  publish_group BOOLEAN NOT NULL DEFAULT FALSE, max_streams INTEGER NOT NULL DEFAULT 1, ptz_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  register_at TIMESTAMP(3), register_expires_at TIMESTAMP(3), heartbeat_at TIMESTAMP(3),
  last_error_code VARCHAR(64), last_error_message TEXT, last_error_at TIMESTAMP(3),
  enabled BOOLEAN NOT NULL DEFAULT FALSE, config_revision BIGINT NOT NULL DEFAULT 1, projection_revision BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL, deleted_at TIMESTAMP(3),
  CONSTRAINT uk_cascade_platform_name UNIQUE (name),
  CONSTRAINT uk_cascade_platform_local_identity UNIQUE (local_device_id, local_domain)
);
CREATE INDEX IF NOT EXISTS idx_cascade_platform_enabled ON gb_cascade_platform (enabled);
CREATE INDEX IF NOT EXISTS idx_cascade_platform_deleted_at ON gb_cascade_platform (deleted_at);

CREATE TABLE IF NOT EXISTS gb_cascade_device_projection (
  id BIGSERIAL PRIMARY KEY, platform_id BIGINT NOT NULL, source_device_id BIGINT NOT NULL,
  published_device_id VARCHAR(20) NOT NULL, name VARCHAR(255), manufacturer VARCHAR(255), model VARCHAR(255),
  owner VARCHAR(255), civil_code VARCHAR(32), address VARCHAR(255), parental INTEGER NOT NULL DEFAULT 0,
  secrecy INTEGER NOT NULL DEFAULT 0, active BOOLEAN NOT NULL DEFAULT TRUE, revision BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL, deleted_at TIMESTAMP(3),
  CONSTRAINT uk_cascade_device_source UNIQUE (platform_id, source_device_id),
  CONSTRAINT uk_cascade_device_published UNIQUE (platform_id, published_device_id)
);
CREATE INDEX IF NOT EXISTS idx_cascade_device_platform_active ON gb_cascade_device_projection (platform_id, active);
CREATE INDEX IF NOT EXISTS idx_cascade_device_deleted_at ON gb_cascade_device_projection (deleted_at);

CREATE TABLE IF NOT EXISTS gb_cascade_channel_projection (
  id BIGSERIAL PRIMARY KEY, platform_id BIGINT NOT NULL, device_projection_id BIGINT NOT NULL,
  source_channel_id BIGINT NOT NULL, published_channel_id VARCHAR(20) NOT NULL, name VARCHAR(255),
  parent_override VARCHAR(20), ptz_allowed BOOLEAN NOT NULL DEFAULT FALSE, active BOOLEAN NOT NULL DEFAULT TRUE,
  revision BIGINT NOT NULL DEFAULT 1, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3),
  CONSTRAINT uk_cascade_channel_source UNIQUE (platform_id, source_channel_id),
  CONSTRAINT uk_cascade_channel_published UNIQUE (platform_id, published_channel_id)
);
CREATE INDEX IF NOT EXISTS idx_cascade_channel_platform_active ON gb_cascade_channel_projection (platform_id, active);
CREATE INDEX IF NOT EXISTS idx_cascade_channel_device_projection ON gb_cascade_channel_projection (device_projection_id);
CREATE INDEX IF NOT EXISTS idx_cascade_channel_deleted_at ON gb_cascade_channel_projection (deleted_at);

CREATE TABLE IF NOT EXISTS gb_cascade_media_session (
  id BIGSERIAL PRIMARY KEY, platform_id BIGINT NOT NULL, dialog_key VARCHAR(512) NOT NULL, call_id VARCHAR(255) NOT NULL,
  local_tag VARCHAR(255), remote_tag VARCHAR(255), cseq BIGINT NOT NULL DEFAULT 0,
  source_device_id BIGINT NOT NULL, source_channel_id BIGINT NOT NULL, published_channel_id VARCHAR(20) NOT NULL,
  profile_version VARCHAR(16), profile_charset VARCHAR(32), sdp_summary TEXT, zlm_node_id BIGINT NOT NULL DEFAULT 0,
  zlm_vhost VARCHAR(128), zlm_app VARCHAR(64), zlm_stream VARCHAR(128), sender_ssrc VARCHAR(32), transport VARCHAR(16),
  remote_ip VARCHAR(45), remote_port INTEGER NOT NULL DEFAULT 0, state VARCHAR(16) NOT NULL DEFAULT 'received',
  failure_code VARCHAR(64), failure_message TEXT, received_at TIMESTAMP(3), answered_at TIMESTAMP(3), active_at TIMESTAMP(3),
  closed_at TIMESTAMP(3), created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_cascade_media_dialog UNIQUE (dialog_key)
);
CREATE INDEX IF NOT EXISTS idx_cascade_media_platform_state ON gb_cascade_media_session (platform_id, state);
CREATE INDEX IF NOT EXISTS idx_cascade_media_call_id ON gb_cascade_media_session (call_id);
