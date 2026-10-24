IF OBJECT_ID(N'gb_cascade_snapshot_session', N'U') IS NULL
  CREATE TABLE [gb_cascade_snapshot_session] (
    [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    [platform_id] BIGINT NOT NULL,
    [upstream_session_id] NVARCHAR(128) NOT NULL,
    [request_key] NVARCHAR(64) NOT NULL,
    [request_fingerprint] NVARCHAR(64) NOT NULL,
    [request_result] NVARCHAR(8) NOT NULL CONSTRAINT df_cascade_snapshot_request_result DEFAULT N'',
    [published_channel_id] NVARCHAR(20) NOT NULL,
    [source_device_id] BIGINT NOT NULL,
    [source_channel_id] BIGINT NOT NULL,
    [source_device_epoch] BIGINT NOT NULL CONSTRAINT df_cascade_snapshot_source_epoch DEFAULT 0,
    [config_revision] BIGINT NOT NULL,
    [projection_revision] BIGINT NOT NULL,
    [state] NVARCHAR(16) NOT NULL CONSTRAINT df_cascade_snapshot_state DEFAULT N'pending',
    [expires_at] DATETIME2(3) NOT NULL,
    [completed_at] DATETIME2(3) NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT uk_cascade_snapshot_session UNIQUE ([platform_id], [upstream_session_id]),
    CONSTRAINT uk_cascade_snapshot_request UNIQUE ([platform_id], [request_key])
  );
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_cascade_snapshot_session') AND name=N'idx_cascade_snapshot_expiry')
  CREATE INDEX idx_cascade_snapshot_expiry ON [gb_cascade_snapshot_session] ([platform_id], [expires_at]);
