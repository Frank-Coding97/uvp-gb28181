CREATE TABLE IF NOT EXISTS gb_cascade_alarm_delivery (
  id BIGSERIAL PRIMARY KEY,
  delivery_key VARCHAR(64) NOT NULL CONSTRAINT uk_cascade_alarm_delivery_key UNIQUE,
  platform_id BIGINT NOT NULL,
  source_device_code VARCHAR(20) NOT NULL,
  source_resource_code VARCHAR(20) NOT NULL,
  published_code VARCHAR(20) NOT NULL,
  profile_version VARCHAR(16) NOT NULL,
  binding_fingerprint VARCHAR(64) NOT NULL,
  payload BYTEA NOT NULL,
  notify_targets TEXT NULL,
  state VARCHAR(16) NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL,
  lease_owner VARCHAR(64) NOT NULL DEFAULT '',
  lease_until TIMESTAMPTZ NULL,
  last_error TEXT NULL,
  sent_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cascade_alarm_delivery_due ON gb_cascade_alarm_delivery (platform_id, state, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_cascade_alarm_delivery_lease ON gb_cascade_alarm_delivery (lease_until);
