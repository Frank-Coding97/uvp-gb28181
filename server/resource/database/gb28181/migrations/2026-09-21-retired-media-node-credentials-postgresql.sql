-- 媒体节点退役：保留「撤销对端 hook」所需的凭据（PostgreSQL）。
-- 与 MySQL 版逐字对应，只换方言；语义说明见 2026-09-21-retired-media-node-credentials.sql。

-- retired-media-node-credentials:start

CREATE TABLE IF NOT EXISTS meta_node_retired (
    id BIGSERIAL,
    media_server_uuid VARCHAR(64) NOT NULL,
    name VARCHAR(64) NOT NULL DEFAULT '',
    host VARCHAR(64) NOT NULL DEFAULT '',
    api_port INTEGER NOT NULL DEFAULT 18080,
    api_secret VARCHAR(128) NOT NULL DEFAULT '',
    retire_reason VARCHAR(255) NOT NULL DEFAULT '',
    unprovision_state VARCHAR(16) NOT NULL DEFAULT 'pending',
    unprovision_attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMP(6),
    retired_at TIMESTAMP(6) NOT NULL,
    updated_at TIMESTAMP(6) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_retired_media_server_uuid UNIQUE (media_server_uuid)
);

CREATE INDEX IF NOT EXISTS idx_retired_unprovision_state ON meta_node_retired (unprovision_state);

COMMENT ON TABLE meta_node_retired IS '已退休媒体节点:仅保留撤销对端 hook 所需的凭据';

-- retired-media-node-credentials:end
