ALTER TABLE gb_device
    ADD COLUMN IF NOT EXISTS zlm_node_id BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_gb_device_zlm_node ON gb_device (zlm_node_id);
