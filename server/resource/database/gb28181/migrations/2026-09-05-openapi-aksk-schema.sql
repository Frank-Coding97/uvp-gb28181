-- openapi-aksk-core:begin
-- T01 foundation only. No clients or permissions are provisioned by migration.
CREATE TABLE IF NOT EXISTS sys_openapi_client (
    id BIGINT NOT NULL AUTO_INCREMENT,
    ak VARCHAR(36) NOT NULL,
    name VARCHAR(100) NOT NULL,
    owner_dept_id BIGINT NOT NULL,
    responsible_user_id BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'disabled',
    secret_ciphertext VARBINARY(64) NOT NULL,
    secret_iv VARBINARY(12) NOT NULL,
    secret_key_id VARCHAR(64) NOT NULL,
    secret_version BIGINT NOT NULL DEFAULT 1,
    auth_epoch BIGINT NOT NULL DEFAULT 1,
    rate_limit INT NOT NULL DEFAULT 10,
    burst INT NOT NULL DEFAULT 20,
    viewer_quota INT NOT NULL DEFAULT 10,
    row_version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT pk_openapi_client PRIMARY KEY (id),
    CONSTRAINT uk_openapi_ak UNIQUE (ak),
    INDEX idx_openapi_client_dept (owner_dept_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS sys_openapi_client_scope (
    client_id BIGINT NOT NULL,
    scope VARCHAR(64) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    updated_by BIGINT NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT pk_openapi_client_scope PRIMARY KEY (client_id, scope)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS sys_openapi_nonce (
    client_id BIGINT NOT NULL,
    nonce VARCHAR(32) NOT NULL,
    accepted_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    CONSTRAINT uk_openapi_nonce PRIMARY KEY (client_id, nonce),
    INDEX idx_openapi_nonce_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS sys_openapi_audit (
    id BIGINT NOT NULL AUTO_INCREMENT,
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
    created_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    CONSTRAINT pk_openapi_audit PRIMARY KEY (id),
    CONSTRAINT uk_openapi_audit_request UNIQUE (request_id),
    INDEX idx_openapi_audit_client_time (client_id, created_at),
    INDEX idx_openapi_audit_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- openapi-aksk-core:end
