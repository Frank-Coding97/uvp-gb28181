-- 在线用户会话表(PostgreSQL 方言,幂等)。refresh 只保存 SHA-256 摘要。
CREATE TABLE IF NOT EXISTS sys_user_sessions (
    sid VARCHAR(36) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    refresh_token_hash CHAR(64) NULL,
    refresh_jti VARCHAR(36) NULL,
    client_ip VARCHAR(50) NOT NULL DEFAULT '',
    login_location VARCHAR(100) NOT NULL DEFAULT '未知',
    user_agent VARCHAR(500) NOT NULL DEFAULT '',
    browser VARCHAR(100) NOT NULL DEFAULT '未知',
    os VARCHAR(100) NOT NULL DEFAULT '未知',
    login_at TIMESTAMP NOT NULL,
    last_active_at TIMESTAMP NOT NULL,
    session_expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    revoke_reason VARCHAR(32) NULL,
    revoked_by BIGINT NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS idx_user_id ON sys_user_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_session_valid ON sys_user_sessions (revoked_at, session_expires_at, login_at);
CREATE INDEX IF NOT EXISTS idx_client_ip ON sys_user_sessions (client_ip);
