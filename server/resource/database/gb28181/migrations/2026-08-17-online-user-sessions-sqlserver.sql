-- 在线用户会话表(SQL Server 方言,幂等)。refresh 只保存 SHA-256 摘要。
IF OBJECT_ID(N'sys_user_sessions', N'U') IS NULL
BEGIN
    CREATE TABLE [sys_user_sessions] (
        [sid] VARCHAR(36) NOT NULL PRIMARY KEY,
        [user_id] BIGINT NOT NULL,
        [refresh_token_hash] CHAR(64) NULL,
        [refresh_jti] VARCHAR(36) NULL,
        [client_ip] VARCHAR(50) NOT NULL CONSTRAINT [df_user_session_client_ip] DEFAULT '',
        [login_location] VARCHAR(100) NOT NULL CONSTRAINT [df_user_session_login_location] DEFAULT '未知',
        [user_agent] VARCHAR(500) NOT NULL CONSTRAINT [df_user_session_user_agent] DEFAULT '',
        [browser] VARCHAR(100) NOT NULL CONSTRAINT [df_user_session_browser] DEFAULT '未知',
        [os] VARCHAR(100) NOT NULL CONSTRAINT [df_user_session_os] DEFAULT '未知',
        [login_at] DATETIME2 NOT NULL,
        [last_active_at] DATETIME2 NOT NULL,
        [session_expires_at] DATETIME2 NOT NULL,
        [revoked_at] DATETIME2 NULL,
        [revoke_reason] VARCHAR(32) NULL,
        [revoked_by] BIGINT NULL,
        [created_at] DATETIME2 NULL,
        [updated_at] DATETIME2 NULL
    );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_user_id' AND object_id = OBJECT_ID(N'sys_user_sessions'))
    CREATE INDEX [idx_user_id] ON [sys_user_sessions] ([user_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_session_valid' AND object_id = OBJECT_ID(N'sys_user_sessions'))
    CREATE INDEX [idx_session_valid] ON [sys_user_sessions] ([revoked_at], [session_expires_at], [login_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_client_ip' AND object_id = OBJECT_ID(N'sys_user_sessions'))
    CREATE INDEX [idx_client_ip] ON [sys_user_sessions] ([client_ip]);
