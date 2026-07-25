-- 2026-07-25: persist GB/T 28181 alarm input/output Catalog resources.
-- PostgreSQL 12+; repeatable migration.

CREATE TABLE IF NOT EXISTS gb_alarm_resource (
  id BIGSERIAL PRIMARY KEY,
  owner_dept_id BIGINT NOT NULL,
  device_id BIGINT NOT NULL DEFAULT 0,
  device_code VARCHAR(20) NOT NULL,
  alarm_code VARCHAR(20) NOT NULL,
  resource_type VARCHAR(16) NOT NULL,
  type_code VARCHAR(3) NOT NULL,
  name VARCHAR(255) NOT NULL,
  raw_parent_ids VARCHAR(512) NOT NULL DEFAULT '',
  status SMALLINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3),
  CONSTRAINT uk_alarm_resource_code UNIQUE (owner_dept_id, device_code, alarm_code)
);

CREATE TABLE IF NOT EXISTS gb_alarm_resource_parent (
  id BIGSERIAL PRIMARY KEY,
  alarm_resource_id BIGINT NOT NULL,
  parent_code VARCHAR(20) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_alarm_resource_parent UNIQUE (alarm_resource_id, parent_code)
);

CREATE TABLE IF NOT EXISTS gb_alarm_binding (
  id BIGSERIAL PRIMARY KEY,
  device_id BIGINT NOT NULL,
  channel_code VARCHAR(20) NOT NULL,
  alarm_resource_id BIGINT NOT NULL,
  source VARCHAR(16) NOT NULL DEFAULT 'manual',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_alarm_binding_channel UNIQUE (device_id, channel_code)
);

ALTER TABLE IF EXISTS gb_catalog_node
  ADD COLUMN IF NOT EXISTS alarm_resource_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_alarm_resource_device ON gb_alarm_resource (owner_dept_id, device_code);
CREATE INDEX IF NOT EXISTS idx_alarm_resource_device_id ON gb_alarm_resource (device_id);
CREATE INDEX IF NOT EXISTS idx_alarm_resource_alarm_code ON gb_alarm_resource (alarm_code);
CREATE INDEX IF NOT EXISTS idx_alarm_resource_type ON gb_alarm_resource (resource_type);
CREATE INDEX IF NOT EXISTS idx_alarm_resource_deleted_at ON gb_alarm_resource (deleted_at);
CREATE INDEX IF NOT EXISTS idx_alarm_parent_resource ON gb_alarm_resource_parent (alarm_resource_id);
CREATE INDEX IF NOT EXISTS idx_alarm_parent_code ON gb_alarm_resource_parent (parent_code);
CREATE INDEX IF NOT EXISTS idx_alarm_binding_device ON gb_alarm_binding (device_id);
CREATE INDEX IF NOT EXISTS idx_alarm_binding_resource ON gb_alarm_binding (alarm_resource_id);
DO $$
BEGIN
  IF to_regclass('gb_catalog_node') IS NOT NULL THEN
    CREATE INDEX IF NOT EXISTS idx_catalog_alarm_resource ON gb_catalog_node (alarm_resource_id);
  END IF;
END $$;
