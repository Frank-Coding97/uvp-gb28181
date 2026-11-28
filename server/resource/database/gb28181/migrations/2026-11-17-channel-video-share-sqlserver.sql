IF OBJECT_ID(N'dbo.gb_channel_video_share', N'U') IS NULL
  CREATE TABLE dbo.gb_channel_video_share (
    id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    channel_pk BIGINT NOT NULL,
    public_id NVARCHAR(64) NOT NULL,
    generation NVARCHAR(64) NOT NULL,
    enabled BIT NOT NULL CONSTRAINT df_video_share_enabled DEFAULT 0,
    activated_at DATETIME2 NULL,
    expires_at DATETIME2 NULL,
    created_by BIGINT NULL,
    device_pk BIGINT NOT NULL,
    device_id NVARCHAR(20) NOT NULL,
    device_epoch BIGINT NOT NULL CONSTRAINT df_video_share_device_epoch DEFAULT 0,
    source NVARCHAR(16) NOT NULL,
    policy_actor_id BIGINT NULL,
    policy_revision BIGINT NOT NULL CONSTRAINT df_video_share_policy_revision DEFAULT 0,
    key_id NVARCHAR(64) NOT NULL,
    revision BIGINT NOT NULL CONSTRAINT df_video_share_revision DEFAULT 1,
    revoked_at DATETIME2 NULL,
    revoke_reason NVARCHAR(64) NOT NULL CONSTRAINT df_video_share_revoke_reason DEFAULT N'',
    created_at DATETIME2 NOT NULL,
    updated_at DATETIME2 NOT NULL,
    CONSTRAINT uk_video_share_channel UNIQUE (channel_pk),
    CONSTRAINT uk_video_share_public_id UNIQUE (public_id)
  );
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_video_share_enabled_expiry' AND object_id = OBJECT_ID(N'dbo.gb_channel_video_share')) CREATE INDEX idx_video_share_enabled_expiry ON dbo.gb_channel_video_share(enabled, expires_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_video_share_created_by' AND object_id = OBJECT_ID(N'dbo.gb_channel_video_share')) CREATE INDEX idx_video_share_created_by ON dbo.gb_channel_video_share(created_by);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_video_share_device_pk' AND object_id = OBJECT_ID(N'dbo.gb_channel_video_share')) CREATE INDEX idx_video_share_device_pk ON dbo.gb_channel_video_share(device_pk);
IF OBJECT_ID(N'dbo.gb_device_video_share_policy', N'U') IS NULL
  CREATE TABLE dbo.gb_device_video_share_policy (
    id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    device_pk BIGINT NOT NULL,
    default_enabled BIT NOT NULL CONSTRAINT df_video_share_default_enabled DEFAULT 0,
    source NVARCHAR(16) NOT NULL,
    policy_actor_id BIGINT NULL,
    policy_revision BIGINT NOT NULL CONSTRAINT df_device_video_share_policy_revision DEFAULT 0,
    initialized_at DATETIME2 NOT NULL,
    created_at DATETIME2 NOT NULL,
    updated_at DATETIME2 NOT NULL,
    CONSTRAINT uk_video_share_device_policy UNIQUE (device_pk)
  );

IF OBJECT_ID(N'dbo.gb_video_share_authority', N'U') IS NULL
  CREATE TABLE dbo.gb_video_share_authority (id BIGINT NOT NULL PRIMARY KEY, revision BIGINT NOT NULL DEFAULT 0);
IF NOT EXISTS (SELECT 1 FROM dbo.gb_video_share_authority WHERE id = 1)
  INSERT INTO dbo.gb_video_share_authority (id, revision) VALUES (1, 0);
