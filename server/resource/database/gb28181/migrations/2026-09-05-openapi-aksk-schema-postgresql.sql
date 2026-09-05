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
