CREATE TABLE IF NOT EXISTS gb_dashboard_layout (
  id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, dashboard_key VARCHAR(32) NOT NULL,
  schema_version INTEGER NOT NULL, revision BIGINT NOT NULL DEFAULT 1, layout_json TEXT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_dashboard_layout_user_key UNIQUE (user_id,dashboard_key)
);
CREATE INDEX IF NOT EXISTS idx_dashboard_layout_updated ON gb_dashboard_layout(updated_at);

CREATE TABLE IF NOT EXISTS gb_sip_metric_minute (
  id BIGSERIAL PRIMARY KEY, bucket_start TIMESTAMP(3) NOT NULL, method VARCHAR(16) NOT NULL,
  direction VARCHAR(8) NOT NULL, request_count BIGINT NOT NULL DEFAULT 0,
  transaction_count BIGINT NOT NULL DEFAULT 0, transaction_success BIGINT NOT NULL DEFAULT 0,
  transaction_failure BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_sip_metric_minute_bucket UNIQUE (bucket_start,method,direction)
);
CREATE INDEX IF NOT EXISTS idx_sip_metric_minute_bucket ON gb_sip_metric_minute(bucket_start);

CREATE TABLE IF NOT EXISTS gb_sip_metric_flush (
  id BIGSERIAL PRIMARY KEY, flush_id VARCHAR(64) NOT NULL, created_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_sip_metric_flush_id UNIQUE (flush_id)
);
CREATE INDEX IF NOT EXISTS idx_sip_metric_flush_created ON gb_sip_metric_flush(created_at);

CREATE TABLE IF NOT EXISTS gb_sip_metric_gap (
  id BIGSERIAL PRIMARY KEY, started_at TIMESTAMP(3) NOT NULL, ended_at TIMESTAMP(3) NOT NULL,
  reason VARCHAR(32) NOT NULL, dropped_count BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMP(3) NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sip_metric_gap_window ON gb_sip_metric_gap(started_at,ended_at);

CREATE TABLE IF NOT EXISTS gb_play_attempt (
  id BIGSERIAL PRIMARY KEY, correlation_id VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL,
  device_code VARCHAR(20) NOT NULL, channel_code VARCHAR(20) NOT NULL, node_id BIGINT NOT NULL DEFAULT 0,
  reused BOOLEAN NOT NULL DEFAULT FALSE, outcome VARCHAR(32) NOT NULL, failure_stage VARCHAR(32) NOT NULL DEFAULT '',
  started_at TIMESTAMP(3) NOT NULL, finished_at TIMESTAMP(3) NULL, created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL, CONSTRAINT uk_play_attempt_correlation UNIQUE (correlation_id)
);
CREATE INDEX IF NOT EXISTS idx_play_attempt_user_started ON gb_play_attempt(user_id,started_at);
CREATE INDEX IF NOT EXISTS idx_play_attempt_device_started ON gb_play_attempt(device_code,started_at);
CREATE INDEX IF NOT EXISTS idx_play_attempt_outcome_started ON gb_play_attempt(outcome,started_at);
