-- openapi-aksk-core:begin
-- T01 foundation only. No clients or permissions are provisioned by migration.
CREATE TABLE IF NOT EXISTS sys_openapi_client (
    id BIGSERIAL NOT NULL,
    ak VARCHAR(36) NOT NULL,
    name VARCHAR(100) NOT NULL,
    owner_dept_id BIGINT NOT NULL,
    responsible_user_id BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'disabled',
    secret_ciphertext BYTEA NOT NULL,
    secret_iv BYTEA NOT NULL,
    secret_key_id VARCHAR(64) NOT NULL,
    secret_version BIGINT NOT NULL DEFAULT 1,
    auth_epoch BIGINT NOT NULL DEFAULT 1,
    rate_limit INT NOT NULL DEFAULT 10,
    burst INT NOT NULL DEFAULT 20,
    viewer_quota INT NOT NULL DEFAULT 10,
    row_version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT pk_openapi_client PRIMARY KEY (id),
    CONSTRAINT uk_openapi_ak UNIQUE (ak)
);

CREATE INDEX IF NOT EXISTS idx_openapi_client_dept ON sys_openapi_client (owner_dept_id);

CREATE TABLE IF NOT EXISTS sys_openapi_client_scope (
    client_id BIGINT NOT NULL,
    scope VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    updated_by BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT pk_openapi_client_scope PRIMARY KEY (client_id, scope)
);

CREATE TABLE IF NOT EXISTS sys_openapi_nonce (
    client_id BIGINT NOT NULL,
    nonce VARCHAR(32) NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT uk_openapi_nonce PRIMARY KEY (client_id, nonce)
);

CREATE INDEX IF NOT EXISTS idx_openapi_nonce_expiry ON sys_openapi_nonce (expires_at);

CREATE TABLE IF NOT EXISTS sys_openapi_audit (
    id BIGSERIAL NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    client_id BIGINT NULL,
    ak_fingerprint VARCHAR(64) NOT NULL DEFAULT '',
    scope VARCHAR(64) NOT NULL DEFAULT '',
    resource_type VARCHAR(32) NOT NULL DEFAULT '',
    resource_id VARCHAR(128) NOT NULL DEFAULT '',
    result VARCHAR(24) NOT NULL,
    reason_class VARCHAR(64) NOT NULL DEFAULT '',
    source VARCHAR(64) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT pk_openapi_audit PRIMARY KEY (id),
    CONSTRAINT uk_openapi_audit_request UNIQUE (request_id)
);

CREATE INDEX IF NOT EXISTS idx_openapi_audit_client_time ON sys_openapi_audit (client_id, created_at);

CREATE INDEX IF NOT EXISTS idx_openapi_audit_time ON sys_openapi_audit (created_at);

-- openapi-aksk-core:end
-- openapi-aksk-media:begin
CREATE TABLE IF NOT EXISTS gb_openapi_play_grant (
    grant_id CHAR(36) COLLATE "C" NOT NULL,
    client_id BIGINT NOT NULL,
    scope VARCHAR(64) COLLATE "C" NOT NULL,
    device_id VARCHAR(20) COLLATE "C" NULL,
    channel_id VARCHAR(20) COLLATE "C" NULL,
    client_epoch BIGINT NOT NULL DEFAULT 1,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    device_epoch BIGINT NOT NULL DEFAULT 1,
    node_uuid VARCHAR(64) COLLATE "C" NULL,
    boot_nonce CHAR(32) COLLATE "C" NULL,
    "schema" VARCHAR(32) COLLATE "C" NULL,
    vhost VARCHAR(128) COLLATE "C" NULL,
    app VARCHAR(64) COLLATE "C" NULL,
    stream VARCHAR(255) COLLATE "C" NULL,
    media_generation BIGINT NULL,
    protocol VARCHAR(16) COLLATE "C" NULL,
    issued_at TIMESTAMPTZ(6) NOT NULL,
    expires_at TIMESTAMPTZ(6) NOT NULL,
    state VARCHAR(16) COLLATE "C" NOT NULL DEFAULT 'pending',
    reason VARCHAR(64) COLLATE "C" NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL,
    updated_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT pk_openapi_play_grant PRIMARY KEY (grant_id),
    CONSTRAINT ck_openapi_grant_state CHECK (state IN ('pending','issued','bound','revoked','expired','failed')),
    CONSTRAINT ck_openapi_grant_epochs CHECK (client_epoch > 0 AND scope_epoch > 0 AND device_epoch > 0),
    CONSTRAINT ck_openapi_grant_binding CHECK (
        state NOT IN ('issued','bound') OR
        (device_id IS NOT NULL AND channel_id IS NOT NULL AND node_uuid IS NOT NULL AND
         boot_nonce IS NOT NULL AND char_length(boot_nonce) = 32 AND "schema" IS NOT NULL AND
         vhost IS NOT NULL AND app IS NOT NULL AND stream IS NOT NULL AND
         media_generation IS NOT NULL AND media_generation > 0 AND protocol IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_openapi_grant_client_state ON gb_openapi_play_grant (client_id, state);
CREATE INDEX IF NOT EXISTS idx_openapi_grant_expires ON gb_openapi_play_grant (expires_at);
CREATE INDEX IF NOT EXISTS idx_openapi_grant_node_boot ON gb_openapi_play_grant (node_uuid, boot_nonce);

CREATE TABLE IF NOT EXISTS gb_openapi_viewer (
    id BIGSERIAL NOT NULL,
    grant_id CHAR(36) COLLATE "C" NOT NULL,
    node_uuid VARCHAR(64) COLLATE "C" NOT NULL,
    boot_nonce CHAR(32) COLLATE "C" NOT NULL,
    identifier VARCHAR(128) COLLATE "C" NOT NULL,
    "schema" VARCHAR(32) COLLATE "C" NOT NULL,
    vhost VARCHAR(128) COLLATE "C" NOT NULL,
    app VARCHAR(64) COLLATE "C" NOT NULL,
    stream VARCHAR(255) COLLATE "C" NOT NULL,
    media_generation BIGINT NOT NULL DEFAULT 0,
    state VARCHAR(16) COLLATE "C" NOT NULL DEFAULT 'pending',
    last_seen_at TIMESTAMPTZ(6) NULL,
    retry_at TIMESTAMPTZ(6) NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error_class VARCHAR(64) COLLATE "C" NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(6) NOT NULL,
    updated_at TIMESTAMPTZ(6) NOT NULL,
    CONSTRAINT pk_openapi_viewer PRIMARY KEY (id),
    CONSTRAINT uk_openapi_viewer_grant UNIQUE (grant_id),
    CONSTRAINT uk_openapi_viewer_identity UNIQUE (node_uuid, boot_nonce, identifier),
    CONSTRAINT ck_openapi_viewer_identity CHECK (node_uuid <> '' AND boot_nonce <> '' AND char_length(boot_nonce) = 32 AND identifier <> ''),
    CONSTRAINT ck_openapi_viewer_state CHECK (state IN ('pending','active','revoke_pending','closed')),
    CONSTRAINT ck_openapi_viewer_media_generation CHECK (media_generation >= 0),
    CONSTRAINT ck_openapi_viewer_attempts CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_openapi_viewer_state_retry ON gb_openapi_viewer (state, retry_at);

ALTER TABLE IF EXISTS gb_device ADD COLUMN IF NOT EXISTS access_epoch BIGINT NOT NULL DEFAULT 1 CHECK (access_epoch > 0);
ALTER TABLE IF EXISTS gb_device ADD COLUMN IF NOT EXISTS legacy_revoked_before TIMESTAMPTZ(0) NULL;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS current_boot_nonce CHAR(32) COLLATE "C" NULL;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS retired_boot_history TEXT NULL;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS runtime_epoch BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS runtime_protocol_version BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS runtime_confirmed_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS runtime_confirmed_at TIMESTAMPTZ(6) NULL;
ALTER TABLE IF EXISTS meta_node ADD COLUMN IF NOT EXISTS runtime_identity_status VARCHAR(16) COLLATE "C" NOT NULL DEFAULT 'unknown';

-- openapi-aksk-media:end
