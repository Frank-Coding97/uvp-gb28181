-- 回退「媒体节点退役凭据」（SQL Server）：整表删除。
-- 语义说明见 2026-09-21-retired-media-node-credentials-down.sql。

IF OBJECT_ID(N'meta_node_retired', N'U') IS NOT NULL
BEGIN
    DROP TABLE [meta_node_retired];
END;
