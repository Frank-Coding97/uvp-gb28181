ALTER TABLE gb_catalog_node
  ADD COLUMN IF NOT EXISTS source_device_id VARCHAR(20) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_owner_source_type_code
  ON gb_catalog_node (owner_dept_id, source_device_id, node_type, code);
-- 可由物理关联确定来源的历史节点安全回填；无法唯一归因的业务节点保留空值。
UPDATE gb_catalog_node n
SET source_device_id = c.device_id
FROM gb_channel c
WHERE n.channel_id = c.id AND n.source_device_id = '' AND c.device_id <> ''
  AND n.node_type = 'channel' AND n.source = 'catalog'
  AND n.owner_dept_id = c.owner_dept_id
  AND n.deleted_at IS NULL AND c.deleted_at IS NULL;
