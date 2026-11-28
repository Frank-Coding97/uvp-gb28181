ALTER TABLE "gb_catalog_node"
  ADD COLUMN "source_device_id" TEXT NOT NULL DEFAULT ''
  CHECK (length("source_device_id") <= 20);
CREATE INDEX IF NOT EXISTS "idx_owner_source_type_code"
  ON "gb_catalog_node" ("owner_dept_id", "source_device_id", "node_type", "code");
-- 可由物理关联确定来源的历史节点安全回填；无法唯一归因的业务节点保留空值。
UPDATE "gb_catalog_node"
SET "source_device_id" = (SELECT c."device_id" FROM "gb_channel" c WHERE c."id" = "gb_catalog_node"."channel_id")
WHERE "source_device_id" = '' AND "channel_id" IS NOT NULL
  AND "node_type" = 'channel' AND "source" = 'catalog' AND "deleted_at" IS NULL
  AND EXISTS (SELECT 1 FROM "gb_channel" c
    WHERE c."id" = "gb_catalog_node"."channel_id" AND c."device_id" <> ''
      AND c."owner_dept_id" = "gb_catalog_node"."owner_dept_id" AND c."deleted_at" IS NULL);
