-- openapi-aksk-core:begin
-- T01 foundation only. No clients or permissions are provisioned by migration.
IF OBJECT_ID(N'dbo.sys_openapi_client', N'U') IS NULL
CREATE TABLE dbo.sys_openapi_client (
    id BIGINT IDENTITY(1,1) NOT NULL,
    ak NVARCHAR(36) COLLATE Latin1_General_100_BIN2 NOT NULL,
    name NVARCHAR(100) COLLATE Latin1_General_100_BIN2 NOT NULL,
    owner_dept_id BIGINT NOT NULL,
    responsible_user_id BIGINT NOT NULL DEFAULT 0,
    status NVARCHAR(16) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT 'disabled',
    secret_ciphertext VARBINARY(64) NOT NULL,
    secret_iv VARBINARY(12) NOT NULL,
    secret_key_id NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    secret_version BIGINT NOT NULL DEFAULT 1,
    auth_epoch BIGINT NOT NULL DEFAULT 1,
    rate_limit INT NOT NULL DEFAULT 10,
    burst INT NOT NULL DEFAULT 20,
    viewer_quota INT NOT NULL DEFAULT 10,
    row_version BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at DATETIME2(6) NOT NULL,
    updated_at DATETIME2(6) NOT NULL,
    CONSTRAINT pk_openapi_client PRIMARY KEY (id),
    CONSTRAINT uk_openapi_ak UNIQUE (ak)
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_client_dept' AND object_id = OBJECT_ID(N'dbo.sys_openapi_client'))
CREATE INDEX idx_openapi_client_dept ON dbo.sys_openapi_client (owner_dept_id);

IF OBJECT_ID(N'dbo.sys_openapi_client_scope', N'U') IS NULL
CREATE TABLE dbo.sys_openapi_client_scope (
    client_id BIGINT NOT NULL,
    scope NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    enabled BIT NOT NULL DEFAULT 0,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    updated_by BIGINT NOT NULL,
    updated_at DATETIME2(6) NOT NULL,
    CONSTRAINT pk_openapi_client_scope PRIMARY KEY (client_id, scope)
);

IF OBJECT_ID(N'dbo.sys_openapi_nonce', N'U') IS NULL
CREATE TABLE dbo.sys_openapi_nonce (
    client_id BIGINT NOT NULL,
    nonce NVARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    accepted_at DATETIME2(6) NOT NULL,
    expires_at DATETIME2(6) NOT NULL,
    CONSTRAINT uk_openapi_nonce PRIMARY KEY (client_id, nonce)
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_nonce_expiry' AND object_id = OBJECT_ID(N'dbo.sys_openapi_nonce'))
CREATE INDEX idx_openapi_nonce_expiry ON dbo.sys_openapi_nonce (expires_at);

IF OBJECT_ID(N'dbo.sys_openapi_audit', N'U') IS NULL
CREATE TABLE dbo.sys_openapi_audit (
    id BIGINT IDENTITY(1,1) NOT NULL,
    request_id NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    client_id BIGINT NULL,
    ak_fingerprint NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    scope NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    resource_type NVARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    resource_id NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    result NVARCHAR(24) COLLATE Latin1_General_100_BIN2 NOT NULL,
    reason_class NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    source NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME2(6) NOT NULL,
    completed_at DATETIME2(6) NULL,
    CONSTRAINT pk_openapi_audit PRIMARY KEY (id),
    CONSTRAINT uk_openapi_audit_request UNIQUE (request_id)
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_audit_client_time' AND object_id = OBJECT_ID(N'dbo.sys_openapi_audit'))
CREATE INDEX idx_openapi_audit_client_time ON dbo.sys_openapi_audit (client_id, created_at);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_audit_time' AND object_id = OBJECT_ID(N'dbo.sys_openapi_audit'))
CREATE INDEX idx_openapi_audit_time ON dbo.sys_openapi_audit (created_at);

-- openapi-aksk-core:end
