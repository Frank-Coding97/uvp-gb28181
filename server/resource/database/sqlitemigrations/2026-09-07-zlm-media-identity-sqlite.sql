-- Complete the media identity omitted from the frozen initial baseline.
-- Existing identity stays unknown; never infer schema or vhost from old keys.
ALTER TABLE gb_zlm_managed_resource ADD COLUMN schema TEXT NOT NULL DEFAULT '' CHECK(length(schema) <= 32);
ALTER TABLE gb_zlm_managed_resource ADD COLUMN vhost TEXT NOT NULL DEFAULT '' CHECK(length(vhost) <= 128);
DROP INDEX uk_gb_zlm_managed_resource_identity;
CREATE UNIQUE INDEX uk_gb_zlm_managed_resource_identity ON gb_zlm_managed_resource(node_id,resource_type,resource_key,schema,vhost,app,stream);
