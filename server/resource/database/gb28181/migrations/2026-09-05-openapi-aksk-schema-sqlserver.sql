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
-- openapi-aksk-media:begin
IF OBJECT_ID(N'dbo.gb_openapi_play_grant', N'U') IS NULL
CREATE TABLE dbo.gb_openapi_play_grant (
    grant_id CHAR(36) COLLATE Latin1_General_100_BIN2 NOT NULL,
    client_id BIGINT NOT NULL,
    scope NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    device_id NVARCHAR(20) COLLATE Latin1_General_100_BIN2 NULL,
    channel_id NVARCHAR(20) COLLATE Latin1_General_100_BIN2 NULL,
    client_epoch BIGINT NOT NULL DEFAULT 1,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    device_epoch BIGINT NOT NULL DEFAULT 1,
    node_uuid NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NULL,
    boot_nonce CHAR(32) COLLATE Latin1_General_100_BIN2 NULL,
    [schema] NVARCHAR(32) COLLATE Latin1_General_100_BIN2 NULL,
    vhost NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NULL,
    app NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NULL,
    stream NVARCHAR(255) COLLATE Latin1_General_100_BIN2 NULL,
    media_generation BIGINT NULL,
    protocol NVARCHAR(16) COLLATE Latin1_General_100_BIN2 NULL,
    issued_at DATETIME2(6) NOT NULL,
    expires_at DATETIME2(6) NOT NULL,
    state NVARCHAR(16) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT N'pending',
    reason NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT N'',
    created_at DATETIME2(6) NOT NULL,
    updated_at DATETIME2(6) NOT NULL,
    CONSTRAINT pk_openapi_play_grant PRIMARY KEY (grant_id),
    CONSTRAINT ck_openapi_grant_state CHECK (state IN (N'pending',N'issued',N'bound',N'revoked',N'expired',N'failed')),
    CONSTRAINT ck_openapi_grant_epochs CHECK (client_epoch > 0 AND scope_epoch > 0 AND device_epoch > 0),
    CONSTRAINT ck_openapi_grant_binding CHECK (
        state NOT IN (N'issued',N'bound') OR
        (device_id IS NOT NULL AND channel_id IS NOT NULL AND node_uuid IS NOT NULL AND
         boot_nonce IS NOT NULL AND LEN(boot_nonce) = 32 AND [schema] IS NOT NULL AND
         device_id <> N'' AND channel_id <> N'' AND node_uuid <> N'' AND boot_nonce <> N'' AND [schema] <> N'' AND
         vhost IS NOT NULL AND vhost <> N'' AND app IS NOT NULL AND app <> N'' AND stream IS NOT NULL AND stream <> N'' AND
         media_generation IS NOT NULL AND media_generation > 0 AND protocol IS NOT NULL AND protocol <> N'')
    )
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_grant_client_state' AND object_id = OBJECT_ID(N'dbo.gb_openapi_play_grant'))
CREATE INDEX idx_openapi_grant_client_state ON dbo.gb_openapi_play_grant (client_id, state);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_grant_expires' AND object_id = OBJECT_ID(N'dbo.gb_openapi_play_grant'))
CREATE INDEX idx_openapi_grant_expires ON dbo.gb_openapi_play_grant (expires_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_grant_node_boot' AND object_id = OBJECT_ID(N'dbo.gb_openapi_play_grant'))
CREATE INDEX idx_openapi_grant_node_boot ON dbo.gb_openapi_play_grant (node_uuid, boot_nonce);

IF OBJECT_ID(N'dbo.gb_openapi_viewer', N'U') IS NULL
CREATE TABLE dbo.gb_openapi_viewer (
    id BIGINT IDENTITY(1,1) NOT NULL,
    grant_id CHAR(36) COLLATE Latin1_General_100_BIN2 NOT NULL,
    node_uuid NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    boot_nonce CHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    identifier NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NOT NULL,
    [schema] NVARCHAR(32) COLLATE Latin1_General_100_BIN2 NOT NULL,
    vhost NVARCHAR(128) COLLATE Latin1_General_100_BIN2 NOT NULL,
    app NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL,
    stream NVARCHAR(255) COLLATE Latin1_General_100_BIN2 NOT NULL,
    media_generation BIGINT NOT NULL,
    state NVARCHAR(16) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT N'pending',
    last_seen_at DATETIME2(6) NULL,
    retry_at DATETIME2(6) NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error_class NVARCHAR(64) COLLATE Latin1_General_100_BIN2 NOT NULL DEFAULT N'',
    created_at DATETIME2(6) NOT NULL,
    updated_at DATETIME2(6) NOT NULL,
    CONSTRAINT pk_openapi_viewer PRIMARY KEY (id),
    CONSTRAINT uk_openapi_viewer_grant UNIQUE (grant_id),
    CONSTRAINT fk_openapi_viewer_grant FOREIGN KEY (grant_id) REFERENCES dbo.gb_openapi_play_grant (grant_id) ON DELETE NO ACTION,
    CONSTRAINT uk_openapi_viewer_identity UNIQUE (node_uuid, boot_nonce, identifier),
    CONSTRAINT ck_openapi_viewer_identity CHECK (node_uuid <> N'' AND boot_nonce <> N'' AND LEN(boot_nonce) = 32 AND identifier <> N''),
    CONSTRAINT ck_openapi_viewer_media_binding CHECK ([schema] <> N'' AND vhost <> N'' AND app <> N'' AND stream <> N'' AND media_generation > 0),
    CONSTRAINT ck_openapi_viewer_state CHECK (state IN (N'pending',N'active',N'revoke_pending',N'closed')),
    CONSTRAINT ck_openapi_viewer_media_generation CHECK (media_generation > 0),
    CONSTRAINT ck_openapi_viewer_attempts CHECK (attempts >= 0)
);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_openapi_viewer_state_retry' AND object_id = OBJECT_ID(N'dbo.gb_openapi_viewer'))
CREATE INDEX idx_openapi_viewer_state_retry ON dbo.gb_openapi_viewer (state, retry_at);

IF COL_LENGTH(N'dbo.gb_device', N'access_epoch') IS NULL
ALTER TABLE dbo.gb_device ADD access_epoch BIGINT NOT NULL CONSTRAINT df_gb_device_access_epoch DEFAULT 1, CONSTRAINT ck_gb_device_access_epoch CHECK (access_epoch > 0);
IF COL_LENGTH(N'dbo.gb_device', N'legacy_revoked_before') IS NULL
ALTER TABLE dbo.gb_device ADD legacy_revoked_before DATETIME2(0) NULL;
IF COL_LENGTH(N'dbo.meta_node', N'current_boot_nonce') IS NULL
ALTER TABLE dbo.meta_node ADD current_boot_nonce CHAR(32) COLLATE Latin1_General_100_BIN2 NULL;
IF COL_LENGTH(N'dbo.meta_node', N'retired_boot_history') IS NULL
ALTER TABLE dbo.meta_node ADD retired_boot_history NVARCHAR(MAX) NULL;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_epoch') IS NULL
ALTER TABLE dbo.meta_node ADD runtime_epoch BIGINT NOT NULL CONSTRAINT df_meta_node_runtime_epoch DEFAULT 0;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_protocol_version') IS NULL
ALTER TABLE dbo.meta_node ADD runtime_protocol_version BIGINT NOT NULL CONSTRAINT df_meta_node_runtime_protocol_version DEFAULT 0;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_confirmed_revision') IS NULL
ALTER TABLE dbo.meta_node ADD runtime_confirmed_revision BIGINT NOT NULL CONSTRAINT df_meta_node_runtime_confirmed_revision DEFAULT 0;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_confirmed_at') IS NULL
ALTER TABLE dbo.meta_node ADD runtime_confirmed_at DATETIME2(6) NULL;
IF COL_LENGTH(N'dbo.meta_node', N'runtime_identity_status') IS NULL
ALTER TABLE dbo.meta_node ADD runtime_identity_status NVARCHAR(16) COLLATE Latin1_General_100_BIN2 NOT NULL CONSTRAINT df_meta_node_runtime_identity_status DEFAULT N'unknown';

-- openapi-aksk-media:end
