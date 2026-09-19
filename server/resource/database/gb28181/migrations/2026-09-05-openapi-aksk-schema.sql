-- openapi-aksk-core:begin
-- T01 foundation only. No clients or permissions are provisioned by migration.
CREATE TABLE IF NOT EXISTS sys_openapi_client (
    id BIGINT NOT NULL AUTO_INCREMENT,
    ak VARCHAR(36) NOT NULL,
    name VARCHAR(100) NOT NULL,
    owner_dept_id BIGINT NOT NULL,
    data_scope TINYINT NOT NULL DEFAULT 3 COMMENT '数据范围 3本部门 4本部门及以下',
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
    CONSTRAINT ck_openapi_client_data_scope CHECK (data_scope IN (3,4)),
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
-- openapi-aksk-media:begin
-- The grant may be created before a media session exists. Once it is issued
-- or bound, every media binding component is mandatory and checked below.
CREATE TABLE IF NOT EXISTS gb_openapi_play_grant (
    grant_id CHAR(36) COLLATE utf8mb4_bin NOT NULL,
    client_id BIGINT NOT NULL,
    scope VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
    device_id VARCHAR(20) COLLATE utf8mb4_bin NULL,
    channel_id VARCHAR(20) COLLATE utf8mb4_bin NULL,
    client_epoch BIGINT NOT NULL DEFAULT 1,
    scope_epoch BIGINT NOT NULL DEFAULT 1,
    device_epoch BIGINT NOT NULL DEFAULT 1,
    node_uuid VARCHAR(64) COLLATE utf8mb4_bin NULL,
    boot_nonce CHAR(32) COLLATE utf8mb4_bin NULL,
    `schema` VARCHAR(32) COLLATE utf8mb4_bin NULL,
    vhost VARCHAR(128) COLLATE utf8mb4_bin NULL,
    app VARCHAR(64) COLLATE utf8mb4_bin NULL,
    stream VARCHAR(255) COLLATE utf8mb4_bin NULL,
    media_generation BIGINT NULL,
    protocol VARCHAR(16) COLLATE utf8mb4_bin NULL,
    issued_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    state VARCHAR(16) COLLATE utf8mb4_bin NOT NULL DEFAULT 'pending',
    reason VARCHAR(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT pk_openapi_play_grant PRIMARY KEY (grant_id),
    CONSTRAINT ck_openapi_grant_state CHECK (state IN ('pending','issued','bound','revoked','expired','failed')),
    CONSTRAINT ck_openapi_grant_epochs CHECK (client_epoch > 0 AND scope_epoch > 0 AND device_epoch > 0),
    CONSTRAINT ck_openapi_grant_binding CHECK (
        state NOT IN ('issued','bound') OR
        (device_id IS NOT NULL AND channel_id IS NOT NULL AND node_uuid IS NOT NULL AND
         boot_nonce IS NOT NULL AND CHAR_LENGTH(boot_nonce) = 32 AND `schema` IS NOT NULL AND
         device_id <> '' AND channel_id <> '' AND node_uuid <> '' AND boot_nonce <> '' AND `schema` <> '' AND
         vhost IS NOT NULL AND vhost <> '' AND app IS NOT NULL AND app <> '' AND stream IS NOT NULL AND stream <> '' AND
         media_generation IS NOT NULL AND media_generation > 0 AND protocol IS NOT NULL AND protocol <> '')
    ),
    INDEX idx_openapi_grant_client_state (client_id, state),
    INDEX idx_openapi_grant_expires (expires_at),
    INDEX idx_openapi_grant_node_boot (node_uuid, boot_nonce)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS gb_openapi_viewer (
    id BIGINT NOT NULL AUTO_INCREMENT,
    grant_id CHAR(36) COLLATE utf8mb4_bin NOT NULL,
    node_uuid VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
    boot_nonce CHAR(32) COLLATE utf8mb4_bin NOT NULL,
    identifier VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    `schema` VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
    vhost VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    app VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
    stream VARCHAR(255) COLLATE utf8mb4_bin NOT NULL,
    media_generation BIGINT NOT NULL,
    state VARCHAR(16) COLLATE utf8mb4_bin NOT NULL DEFAULT 'pending',
    last_seen_at DATETIME(6) NULL,
    retry_at DATETIME(6) NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error_class VARCHAR(64) COLLATE utf8mb4_bin NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT pk_openapi_viewer PRIMARY KEY (id),
    CONSTRAINT uk_openapi_viewer_grant UNIQUE (grant_id),
    CONSTRAINT fk_openapi_viewer_grant FOREIGN KEY (grant_id) REFERENCES gb_openapi_play_grant (grant_id) ON DELETE RESTRICT,
    CONSTRAINT uk_openapi_viewer_identity UNIQUE (node_uuid, boot_nonce, identifier),
    CONSTRAINT ck_openapi_viewer_identity CHECK (node_uuid <> '' AND boot_nonce <> '' AND CHAR_LENGTH(boot_nonce) = 32 AND identifier <> ''),
    CONSTRAINT ck_openapi_viewer_media_binding CHECK (`schema` <> '' AND vhost <> '' AND app <> '' AND stream <> '' AND media_generation > 0),
    CONSTRAINT ck_openapi_viewer_state CHECK (state IN ('pending','active','revoke_pending','closed')),
    CONSTRAINT ck_openapi_viewer_media_generation CHECK (media_generation > 0),
    CONSTRAINT ck_openapi_viewer_attempts CHECK (attempts >= 0),
    INDEX idx_openapi_viewer_state_retry (state, retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- MySQL 8 has no portable ADD COLUMN IF NOT EXISTS. The information_schema
-- guards keep upgrades idempotent without changing any existing device/node
-- value; new rows receive only the neutral security defaults below.
SET @openapi_schema_name := DATABASE();
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='gb_device' AND column_name='access_epoch')=0,
    'ALTER TABLE `gb_device` ADD COLUMN `access_epoch` BIGINT NOT NULL DEFAULT 1 CHECK (`access_epoch` > 0)', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='gb_device' AND column_name='legacy_revoked_before')=0,
    'ALTER TABLE `gb_device` ADD COLUMN `legacy_revoked_before` DATETIME NULL', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='current_boot_nonce')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `current_boot_nonce` CHAR(32) NULL', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='retired_boot_history')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `retired_boot_history` TEXT NULL', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_epoch')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `runtime_epoch` BIGINT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_protocol_version')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `runtime_protocol_version` BIGINT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_confirmed_revision')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `runtime_confirmed_revision` BIGINT NOT NULL DEFAULT 0', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_confirmed_at')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `runtime_confirmed_at` DATETIME(6) NULL', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;
SET @openapi_sql := IF((SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=@openapi_schema_name AND table_name='meta_node' AND column_name='runtime_identity_status')=0,
    'ALTER TABLE `meta_node` ADD COLUMN `runtime_identity_status` VARCHAR(16) NOT NULL DEFAULT ''unknown''', 'SELECT 1');
PREPARE openapi_stmt FROM @openapi_sql;
EXECUTE openapi_stmt;
DEALLOCATE PREPARE openapi_stmt;

-- openapi-aksk-media:end
