-- User-private multi-screen playback schemes. SQL Server 2017+, repeatable.
IF OBJECT_ID(N'gb_playback_scheme', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_playback_scheme] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [owner_user_id] BIGINT NOT NULL,
    [owner_dept_id] BIGINT NOT NULL,
    [name] NVARCHAR(64) NOT NULL,
    [layout_size] SMALLINT NOT NULL,
    [slot_count] INT NOT NULL CONSTRAINT [df_playback_scheme_slot_count] DEFAULT 0,
    [created_by] BIGINT NOT NULL,
    [updated_by] BIGINT NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_playback_scheme] PRIMARY KEY ([id]),
    CONSTRAINT [uk_playback_scheme_owner_name] UNIQUE ([owner_user_id], [name])
  );
  CREATE INDEX [idx_playback_scheme_owner_updated] ON [gb_playback_scheme] ([owner_user_id], [updated_at]);
  CREATE INDEX [idx_playback_scheme_dept] ON [gb_playback_scheme] ([owner_dept_id]);
END;

IF OBJECT_ID(N'gb_playback_scheme_slot', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_playback_scheme_slot] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [scheme_id] BIGINT NOT NULL,
    [slot_index] INT NOT NULL,
    [device_code] NVARCHAR(20) NOT NULL,
    [channel_code] NVARCHAR(20) NOT NULL,
    [device_name_snapshot] NVARCHAR(255) NOT NULL,
    [channel_name_snapshot] NVARCHAR(255) NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_playback_scheme_slot] PRIMARY KEY ([id]),
    CONSTRAINT [uk_playback_scheme_slot] UNIQUE ([scheme_id], [slot_index])
  );
  CREATE INDEX [idx_playback_scheme_slot_scheme] ON [gb_playback_scheme_slot] ([scheme_id]);
END;
