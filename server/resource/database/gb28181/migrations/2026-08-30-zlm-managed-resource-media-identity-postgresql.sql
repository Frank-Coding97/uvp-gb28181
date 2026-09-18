-- Add the complete non-secret ZLM media identity to the management ledger.
-- Existing rows intentionally remain empty/unknown; no identity is inferred.
ALTER TABLE IF EXISTS gb_zlm_managed_resource
  ADD COLUMN IF NOT EXISTS "schema" VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS gb_zlm_managed_resource
  ADD COLUMN IF NOT EXISTS vhost VARCHAR(128) NOT NULL DEFAULT '';

-- The original key omitted media identity. Replace it with the exact tuple.
ALTER TABLE IF EXISTS gb_zlm_managed_resource
  DROP CONSTRAINT IF EXISTS uk_gb_zlm_managed_resource_identity;
DROP INDEX IF EXISTS uk_gb_zlm_managed_resource_identity;
CREATE UNIQUE INDEX IF NOT EXISTS uk_gb_zlm_managed_resource_identity
  ON gb_zlm_managed_resource(node_id,resource_type,resource_key,"schema",vhost,app,stream);
