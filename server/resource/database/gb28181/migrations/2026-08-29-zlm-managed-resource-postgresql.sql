-- Management provenance ledger for ZLM resources (PostgreSQL).
CREATE TABLE IF NOT EXISTS gb_zlm_managed_resource (
  id BIGSERIAL PRIMARY KEY,
  node_id BIGINT NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_key VARCHAR(255) NOT NULL,
  app VARCHAR(64) NOT NULL DEFAULT '',
  stream VARCHAR(255) NOT NULL DEFAULT '',
  identity_fingerprint CHAR(64) NOT NULL,
  summary VARCHAR(512) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  last_observed_at TIMESTAMP(3),
  tombstoned_at TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_gb_zlm_managed_resource_identity UNIQUE (node_id,resource_type,resource_key)
);
CREATE INDEX IF NOT EXISTS idx_gb_zlm_managed_resource_observed ON gb_zlm_managed_resource(node_id,last_observed_at);
CREATE INDEX IF NOT EXISTS idx_gb_zlm_managed_resource_tombstone ON gb_zlm_managed_resource(node_id,tombstoned_at);
