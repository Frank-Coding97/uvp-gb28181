-- GB28181 cascade tables. SQL Server 2017+, repeatable for a new cascade installation.
IF OBJECT_ID(N'gb_cascade_platform', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_cascade_platform] (
    [id] BIGINT IDENTITY(1,1) NOT NULL, [name] NVARCHAR(128) NOT NULL, [upstream_server_id] NVARCHAR(20) NOT NULL,
    [upstream_domain] NVARCHAR(255) NOT NULL, [host] NVARCHAR(255) NOT NULL, [port] INT NOT NULL,
    [local_device_id] NVARCHAR(20) NOT NULL, [local_domain] NVARCHAR(255) NOT NULL, [local_sip_ip] NVARCHAR(45) NOT NULL, [local_sip_port] INT NOT NULL,
    [media_advertise_ip] NVARCHAR(45) NULL, [auth_username] NVARCHAR(255) NULL, [secret_nonce] VARBINARY(64) NULL,
    [secret_ciphertext] VARBINARY(MAX) NULL, [secret_alg] NVARCHAR(32) NULL, [secret_key_version] NVARCHAR(64) NULL,
    [profile_override] NVARCHAR(16) NOT NULL CONSTRAINT [df_cascade_platform_profile_override] DEFAULT N'auto', [reported_gb_version] NVARCHAR(16) NULL,
    [reported_gb_version_at] DATETIME2(3) NULL, [effective_version] NVARCHAR(16) NOT NULL CONSTRAINT [df_cascade_platform_effective_version] DEFAULT N'2016',
    [effective_version_source] NVARCHAR(32) NOT NULL CONSTRAINT [df_cascade_platform_effective_source] DEFAULT N'default', [effective_version_at] DATETIME2(3) NULL,
    [charset_override] NVARCHAR(32) NULL, [register_expires] INT NOT NULL CONSTRAINT [df_cascade_platform_register_expires] DEFAULT 3600,
    [keepalive_interval] INT NOT NULL CONSTRAINT [df_cascade_platform_keepalive_interval] DEFAULT 60, [retry_policy] NVARCHAR(MAX) NULL,
    [transport] NVARCHAR(16) NOT NULL CONSTRAINT [df_cascade_platform_transport] DEFAULT N'UDP', [catalog_batch_size] INT NOT NULL CONSTRAINT [df_cascade_platform_batch_size] DEFAULT 100,
    [publish_platform] BIT NOT NULL CONSTRAINT [df_cascade_platform_publish_platform] DEFAULT 0, [publish_civil] BIT NOT NULL CONSTRAINT [df_cascade_platform_publish_civil] DEFAULT 0,
    [publish_group] BIT NOT NULL CONSTRAINT [df_cascade_platform_publish_group] DEFAULT 0, [max_streams] INT NOT NULL CONSTRAINT [df_cascade_platform_max_streams] DEFAULT 1,
    [ptz_enabled] BIT NOT NULL CONSTRAINT [df_cascade_platform_ptz_enabled] DEFAULT 0, [register_at] DATETIME2(3) NULL,
    [register_expires_at] DATETIME2(3) NULL, [heartbeat_at] DATETIME2(3) NULL, [last_error_code] NVARCHAR(64) NULL,
    [last_error_message] NVARCHAR(MAX) NULL, [last_error_at] DATETIME2(3) NULL, [enabled] BIT NOT NULL CONSTRAINT [df_cascade_platform_enabled] DEFAULT 0,
    [config_revision] BIGINT NOT NULL CONSTRAINT [df_cascade_platform_config_revision] DEFAULT 1,
    [projection_revision] BIGINT NOT NULL CONSTRAINT [df_cascade_platform_projection_revision] DEFAULT 0, [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL, [deleted_at] DATETIME2(3) NULL,
    CONSTRAINT [pk_gb_cascade_platform] PRIMARY KEY ([id]), CONSTRAINT [uk_cascade_platform_name] UNIQUE ([name]),
    CONSTRAINT [uk_cascade_platform_local_identity] UNIQUE ([local_device_id], [local_domain])
  );
  CREATE INDEX [idx_cascade_platform_enabled] ON [gb_cascade_platform] ([enabled]);
  CREATE INDEX [idx_cascade_platform_deleted_at] ON [gb_cascade_platform] ([deleted_at]);
END;

IF OBJECT_ID(N'gb_cascade_device_projection', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_cascade_device_projection] (
    [id] BIGINT IDENTITY(1,1) NOT NULL, [platform_id] BIGINT NOT NULL, [source_device_id] BIGINT NOT NULL,
    [published_device_id] NVARCHAR(20) NOT NULL, [name] NVARCHAR(255) NULL, [manufacturer] NVARCHAR(255) NULL,
    [model] NVARCHAR(255) NULL, [owner] NVARCHAR(255) NULL, [civil_code] NVARCHAR(32) NULL, [address] NVARCHAR(255) NULL,
    [parental] INT NOT NULL CONSTRAINT [df_cascade_device_parental] DEFAULT 0, [secrecy] INT NOT NULL CONSTRAINT [df_cascade_device_secrecy] DEFAULT 0,
    [active] BIT NOT NULL CONSTRAINT [df_cascade_device_active] DEFAULT 1, [revision] BIGINT NOT NULL CONSTRAINT [df_cascade_device_revision] DEFAULT 1,
    [created_at] DATETIME2(3) NOT NULL, [updated_at] DATETIME2(3) NOT NULL, [deleted_at] DATETIME2(3) NULL,
    CONSTRAINT [pk_gb_cascade_device_projection] PRIMARY KEY ([id]), CONSTRAINT [uk_cascade_device_source] UNIQUE ([platform_id], [source_device_id]),
    CONSTRAINT [uk_cascade_device_published] UNIQUE ([platform_id], [published_device_id])
  );
  CREATE INDEX [idx_cascade_device_platform_active] ON [gb_cascade_device_projection] ([platform_id], [active]);
  CREATE INDEX [idx_cascade_device_deleted_at] ON [gb_cascade_device_projection] ([deleted_at]);
END;

IF OBJECT_ID(N'gb_cascade_channel_projection', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_cascade_channel_projection] (
    [id] BIGINT IDENTITY(1,1) NOT NULL, [platform_id] BIGINT NOT NULL, [device_projection_id] BIGINT NOT NULL,
    [source_channel_id] BIGINT NOT NULL, [published_channel_id] NVARCHAR(20) NOT NULL, [name] NVARCHAR(255) NULL,
    [parent_override] NVARCHAR(20) NULL, [ptz_allowed] BIT NOT NULL CONSTRAINT [df_cascade_channel_ptz_allowed] DEFAULT 0,
    [active] BIT NOT NULL CONSTRAINT [df_cascade_channel_active] DEFAULT 1, [revision] BIGINT NOT NULL CONSTRAINT [df_cascade_channel_revision] DEFAULT 1,
    [created_at] DATETIME2(3) NOT NULL, [updated_at] DATETIME2(3) NOT NULL, [deleted_at] DATETIME2(3) NULL,
    CONSTRAINT [pk_gb_cascade_channel_projection] PRIMARY KEY ([id]), CONSTRAINT [uk_cascade_channel_source] UNIQUE ([platform_id], [source_channel_id]),
    CONSTRAINT [uk_cascade_channel_published] UNIQUE ([platform_id], [published_channel_id])
  );
  CREATE INDEX [idx_cascade_channel_platform_active] ON [gb_cascade_channel_projection] ([platform_id], [active]);
  CREATE INDEX [idx_cascade_channel_device_projection] ON [gb_cascade_channel_projection] ([device_projection_id]);
  CREATE INDEX [idx_cascade_channel_deleted_at] ON [gb_cascade_channel_projection] ([deleted_at]);
END;

IF OBJECT_ID(N'gb_cascade_media_session', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_cascade_media_session] (
    [id] BIGINT IDENTITY(1,1) NOT NULL, [platform_id] BIGINT NOT NULL, [dialog_key] NVARCHAR(512) NOT NULL,
    [call_id] NVARCHAR(255) NOT NULL, [local_tag] NVARCHAR(255) NULL, [remote_tag] NVARCHAR(255) NULL,
    [cseq] BIGINT NOT NULL CONSTRAINT [df_cascade_media_cseq] DEFAULT 0, [source_device_id] BIGINT NOT NULL,
    [source_channel_id] BIGINT NOT NULL, [published_channel_id] NVARCHAR(20) NOT NULL, [profile_version] NVARCHAR(16) NULL,
    [profile_charset] NVARCHAR(32) NULL, [sdp_summary] NVARCHAR(MAX) NULL, [zlm_node_id] BIGINT NOT NULL CONSTRAINT [df_cascade_media_node] DEFAULT 0,
    [zlm_vhost] NVARCHAR(128) NULL, [zlm_app] NVARCHAR(64) NULL, [zlm_stream] NVARCHAR(128) NULL, [sender_ssrc] NVARCHAR(32) NULL,
    [transport] NVARCHAR(16) NULL, [remote_ip] NVARCHAR(45) NULL, [remote_port] INT NOT NULL CONSTRAINT [df_cascade_media_remote_port] DEFAULT 0,
    [state] NVARCHAR(16) NOT NULL CONSTRAINT [df_cascade_media_state] DEFAULT N'received', [failure_code] NVARCHAR(64) NULL,
    [failure_message] NVARCHAR(MAX) NULL, [received_at] DATETIME2(3) NULL, [answered_at] DATETIME2(3) NULL,
    [active_at] DATETIME2(3) NULL, [closed_at] DATETIME2(3) NULL, [created_at] DATETIME2(3) NOT NULL, [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_cascade_media_session] PRIMARY KEY ([id]), CONSTRAINT [uk_cascade_media_dialog] UNIQUE ([dialog_key])
  );
  CREATE INDEX [idx_cascade_media_platform_state] ON [gb_cascade_media_session] ([platform_id], [state]);
  CREATE INDEX [idx_cascade_media_call_id] ON [gb_cascade_media_session] ([call_id]);
END;
