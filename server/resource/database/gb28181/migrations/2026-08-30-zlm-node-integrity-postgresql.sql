-- Durable ZLM node revision and fail-closed endpoint recovery state (PostgreSQL 12+).
-- ADD COLUMN IF NOT EXISTS keeps upgrades and repeated bootstrap runs safe.
ALTER TABLE IF EXISTS meta_node
  ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE IF EXISTS meta_node
  ADD COLUMN IF NOT EXISTS recovery_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE IF EXISTS meta_node
  ADD COLUMN IF NOT EXISTS recovery_reason VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS meta_node
  ADD COLUMN IF NOT EXISTS recovery_fingerprint CHAR(64) NOT NULL DEFAULT '';
