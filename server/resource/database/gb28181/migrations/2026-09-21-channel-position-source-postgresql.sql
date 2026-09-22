-- 通道坐标来源标记 gb_channel.position_source / position_updated_at —— PostgreSQL 方言.
-- 与 2026-09-21-channel-position-source.sql 等价；语义说明见该文件。

ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS position_source VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS position_updated_at TIMESTAMP(3);

COMMENT ON COLUMN gb_channel.position_source IS '通道坐标来源 catalog/mobile/manual,空=无坐标';
COMMENT ON COLUMN gb_channel.position_updated_at IS '通道坐标最近更新时间';

-- 存量数据回填：把已有非零坐标标成 catalog（此前只有目录这一条通路）。
-- ⛔ 只在 position_source 为空时回填，重跑不覆盖 mobile/manual。
UPDATE gb_channel
SET position_source = 'catalog'
WHERE position_source = '' AND (longitude <> 0 OR latitude <> 0);
