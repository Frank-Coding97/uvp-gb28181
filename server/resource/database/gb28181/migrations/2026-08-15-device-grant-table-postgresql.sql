-- 设备共享授权表(PostgreSQL 方言,幂等)
CREATE TABLE IF NOT EXISTS gb_device_grant (
    id BIGSERIAL PRIMARY KEY,
    device_id INTEGER NOT NULL,
    target_type VARCHAR(16) NOT NULL DEFAULT '',
    target_id INTEGER NOT NULL DEFAULT 0,
    created_by INTEGER DEFAULT 0,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_device_target ON gb_device_grant (device_id, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_target ON gb_device_grant (target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_device ON gb_device_grant (device_id);
CREATE INDEX IF NOT EXISTS idx_deleted_at ON gb_device_grant (deleted_at);
