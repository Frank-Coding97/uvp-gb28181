-- 回退「媒体节点退役凭据」（PostgreSQL）：整表删除。
-- 语义说明见 2026-09-21-retired-media-node-credentials-down.sql。

DROP TABLE IF EXISTS meta_node_retired;
