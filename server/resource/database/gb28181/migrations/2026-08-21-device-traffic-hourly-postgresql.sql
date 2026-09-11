CREATE TABLE IF NOT EXISTS gb_device_traffic_hourly (
  id BIGSERIAL PRIMARY KEY,
  stat_hour TIMESTAMP(6) WITH TIME ZONE NOT NULL,
  device_code VARCHAR(64) NOT NULL,
  channel_code VARCHAR(64) NOT NULL DEFAULT '',
  owner_dept_id BIGINT NOT NULL DEFAULT 0,
  upstream_bytes BIGINT NOT NULL DEFAULT 0,
  downstream_bytes BIGINT NOT NULL DEFAULT 0,
  upstream_duration_seconds BIGINT NOT NULL DEFAULT 0,
  downstream_duration_seconds BIGINT NOT NULL DEFAULT 0,
  upstream_sessions BIGINT NOT NULL DEFAULT 0,
  downstream_sessions BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) WITH TIME ZONE NULL,
  updated_at TIMESTAMP(3) WITH TIME ZONE NULL,
  CONSTRAINT uk_traffic_hourly_scope UNIQUE (stat_hour,device_code,channel_code)
);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_device ON gb_device_traffic_hourly (device_code,stat_hour);
CREATE INDEX IF NOT EXISTS idx_traffic_hourly_owner_dept ON gb_device_traffic_hourly (owner_dept_id);
