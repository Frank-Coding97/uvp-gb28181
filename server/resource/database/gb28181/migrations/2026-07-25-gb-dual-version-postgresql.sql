-- 2026-07-25: GB28181 2016/2022 profile archive and control-state facts.
-- PostgreSQL 12+; all DDL is repeatable.

ALTER TABLE gb_device
    ADD COLUMN IF NOT EXISTS reported_version VARCHAR(8) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reported_version_at TIMESTAMP(3),
    ADD COLUMN IF NOT EXISTS protocol_override VARCHAR(8) NOT NULL DEFAULT 'auto',
    ADD COLUMN IF NOT EXISTS effective_version VARCHAR(8) NOT NULL DEFAULT '2016',
    ADD COLUMN IF NOT EXISTS effective_version_source VARCHAR(16) NOT NULL DEFAULT 'default',
    ADD COLUMN IF NOT EXISTS effective_version_at TIMESTAMP(3);

ALTER TABLE gb_ptz_operation
    ADD COLUMN IF NOT EXISTS profile_version VARCHAR(8),
    ADD COLUMN IF NOT EXISTS profile_charset VARCHAR(16),
    ADD COLUMN IF NOT EXISTS target_scope VARCHAR(16),
    ADD COLUMN IF NOT EXISTS target_code VARCHAR(20);

CREATE TABLE IF NOT EXISTS gb_device_control_state (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL DEFAULT 0,
    target_scope VARCHAR(16) NOT NULL,
    target_code VARCHAR(20) NOT NULL,
    record_state VARCHAR(8) NOT NULL DEFAULT 'unknown',
    guard_state VARCHAR(8) NOT NULL DEFAULT 'unknown',
    freshness VARCHAR(8) NOT NULL DEFAULT 'unknown',
    observed_at TIMESTAMP(3) NOT NULL,
    source VARCHAR(32) NOT NULL DEFAULT 'device_status',
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_control_state_target UNIQUE (target_scope, target_code)
);

CREATE INDEX IF NOT EXISTS idx_control_state_device_target
    ON gb_device_control_state (device_id, target_scope, target_code);
CREATE INDEX IF NOT EXISTS idx_control_state_channel
    ON gb_device_control_state (channel_id);
CREATE INDEX IF NOT EXISTS idx_ptz_operation_target
    ON gb_ptz_operation (device_code, target_scope, target_code, status);

UPDATE gb_device
SET protocol_override = COALESCE(NULLIF(protocol_override, ''), 'auto'),
    effective_version = COALESCE(NULLIF(effective_version, ''), '2016'),
    effective_version_source = COALESCE(NULLIF(effective_version_source, ''), 'default')
WHERE protocol_override = '' OR effective_version = '' OR effective_version_source = '';

UPDATE gb_ptz_operation
SET profile_version = COALESCE(NULLIF(profile_version, ''), '2016'),
    profile_charset = COALESCE(NULLIF(profile_charset, ''), 'GB2312'),
    target_scope = COALESCE(NULLIF(target_scope, ''), 'channel'),
    target_code = COALESCE(NULLIF(target_code, ''), channel_code)
WHERE profile_version IS NULL OR profile_version = ''
   OR profile_charset IS NULL OR profile_charset = ''
   OR target_scope IS NULL OR target_scope = ''
   OR target_code IS NULL OR target_code = '';
