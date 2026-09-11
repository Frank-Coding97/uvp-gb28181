-- 2026-07-24: GB28181 home-position response lifecycle (SQL Server).
-- Idempotent upgrade: legacy gb_ptz_state.home_* columns are intentionally retained.

IF OBJECT_ID(N'gb_ptz_operation', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_operation] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [operation_id] NVARCHAR(64) NOT NULL,
        [idempotency_key] NVARCHAR(128) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [device_code] NVARCHAR(20) NOT NULL,
        [channel_id] BIGINT NOT NULL,
        [channel_code] NVARCHAR(20) NOT NULL,
        [cmd_type] NVARCHAR(64) NOT NULL,
        [action] NVARCHAR(64),
        [payload_json] NVARCHAR(MAX),
        [sn] INT NOT NULL,
        [call_id] NVARCHAR(255),
        [cseq] NVARCHAR(64),
        [sip_status] INT NOT NULL CONSTRAINT [df_ptz_operation_sip_status] DEFAULT 0,
        [device_result] NVARCHAR(32),
        [device_error] NVARCHAR(MAX),
        [status] NVARCHAR(16) NOT NULL,
        [attempt] INT NOT NULL CONSTRAINT [df_ptz_operation_attempt] DEFAULT 1,
        [response_required] BIT NOT NULL CONSTRAINT [df_ptz_operation_response_required] DEFAULT 0,
        [max_attempts] INT NOT NULL CONSTRAINT [df_ptz_operation_max_attempts] DEFAULT 1,
        [error_code] NVARCHAR(64),
        [error_message] NVARCHAR(MAX),
        [actor_id] BIGINT NOT NULL CONSTRAINT [df_ptz_operation_actor_id] DEFAULT 0,
        [actor_dept_id] BIGINT NOT NULL CONSTRAINT [df_ptz_operation_actor_dept_id] DEFAULT 0,
        [created_at] DATETIME2(3) NOT NULL,
        [sent_at] DATETIME2(3),
        [completed_at] DATETIME2(3),
        [queue_deadline_at] DATETIME2(3),
        [dispatch_started_at] DATETIME2(3),
        [transport_deadline_at] DATETIME2(3),
        [deadline_at] DATETIME2(3),
        [next_attempt_at] DATETIME2(3),
        [response_call_id] NVARCHAR(255),
        [response_cseq] NVARCHAR(64),
        [response_at] DATETIME2(3),
        [response_has_data] BIT,
        [trigger_operation_id] NVARCHAR(64),
        [reconcile_operation_id] NVARCHAR(64),
        CONSTRAINT [pk_ptz_operation] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_operation_id] UNIQUE ([operation_id]),
        CONSTRAINT [uk_ptz_operation_idempotency] UNIQUE ([channel_id], [idempotency_key])
    );
END;

IF OBJECT_ID(N'gb_ptz_state', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_state] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL,
        [channel_code] NVARCHAR(20) NOT NULL,
        [pan] DECIMAL(18,6),
        [tilt] DECIMAL(18,6),
        [zoom] DECIMAL(18,6),
        [focus] DECIMAL(18,6),
        [iris] DECIMAL(18,6),
        [device_time] DATETIME2(3),
        [received_at] DATETIME2(3) NOT NULL,
        [source_sn] INT NOT NULL CONSTRAINT [df_ptz_state_source_sn] DEFAULT 0,
        [freshness] NVARCHAR(16) NOT NULL CONSTRAINT [df_ptz_state_freshness] DEFAULT 'unknown',
        [dedupe_key] NVARCHAR(128),
        [raw_summary] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_ptz_state] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_state_channel] UNIQUE ([channel_id])
    );
END;

IF OBJECT_ID(N'gb_ptz_preset', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_preset] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL,
        [preset_id] INT NOT NULL,
        [name] NVARCHAR(255),
        [status] NVARCHAR(16) NOT NULL CONSTRAINT [df_ptz_preset_status] DEFAULT 'unknown',
        [last_operation_id] NVARCHAR(64),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_ptz_preset] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_preset_channel_number] UNIQUE ([channel_id], [preset_id])
    );
END;

IF OBJECT_ID(N'gb_ptz_cruise_track', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_cruise_track] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL,
        [track_id] INT NOT NULL,
        [name] NVARCHAR(255),
        [enabled] BIT,
        [detail_json] NVARCHAR(MAX),
        [raw_summary] NVARCHAR(MAX),
        [device_time] DATETIME2(3),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_ptz_cruise_track] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_cruise_channel_track] UNIQUE ([channel_id], [track_id])
    );
END;

IF OBJECT_ID(N'gb_ptz_operation_attempt', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_operation_attempt] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [operation_id] BIGINT NOT NULL,
        [attempt_no] INT NOT NULL,
        [sn] INT NOT NULL,
        [status] NVARCHAR(16) NOT NULL,
        [call_id] NVARCHAR(255),
        [cseq] NVARCHAR(64),
        [sip_status] INT NOT NULL CONSTRAINT [df_ptz_attempt_sip_status] DEFAULT 0,
        [started_at] DATETIME2(3) NOT NULL,
        [lease_until] DATETIME2(3) NOT NULL,
        [sent_at] DATETIME2(3),
        [completed_at] DATETIME2(3),
        [error_code] NVARCHAR(64),
        [error_message] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_ptz_operation_attempt] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_operation_attempt] UNIQUE ([operation_id], [attempt_no])
    );
END;

IF OBJECT_ID(N'gb_ptz_home_position', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_ptz_home_position] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL,
        [channel_code] NVARCHAR(20) NOT NULL,
        [enabled] BIT NOT NULL,
        [reset_time] INT,
        [preset_id] INT,
        [enabled_encoding] NVARCHAR(32) NOT NULL CONSTRAINT [df_ptz_home_enabled_encoding] DEFAULT 'numeric',
        [confirmed_at] DATETIME2(3) NOT NULL,
        [source] NVARCHAR(32) NOT NULL,
        [verification] NVARCHAR(16) NOT NULL,
        [source_sn] INT NOT NULL CONSTRAINT [df_ptz_home_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64),
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_ptz_home_source_seq] DEFAULT 0,
        [raw_summary] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_ptz_home_position] PRIMARY KEY ([id]),
        CONSTRAINT [uk_ptz_home_position_channel] UNIQUE ([channel_id])
    );
END;

IF COL_LENGTH(N'gb_ptz_operation', N'response_required') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [response_required] BIT NOT NULL CONSTRAINT [df_ptz_operation_response_required] DEFAULT 0;
IF COL_LENGTH(N'gb_ptz_operation', N'max_attempts') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [max_attempts] INT NOT NULL CONSTRAINT [df_ptz_operation_max_attempts] DEFAULT 1;
IF COL_LENGTH(N'gb_ptz_operation', N'queue_deadline_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [queue_deadline_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'dispatch_started_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [dispatch_started_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'transport_deadline_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [transport_deadline_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'deadline_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [deadline_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'next_attempt_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [next_attempt_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'response_call_id') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [response_call_id] NVARCHAR(255) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'response_cseq') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [response_cseq] NVARCHAR(64) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'response_at') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [response_at] DATETIME2(3) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'response_has_data') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [response_has_data] BIT NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'trigger_operation_id') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [trigger_operation_id] NVARCHAR(64) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'reconcile_operation_id') IS NULL
    ALTER TABLE [gb_ptz_operation] ADD [reconcile_operation_id] NVARCHAR(64) NULL;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'uk_ptz_operation_id')
    CREATE UNIQUE INDEX [uk_ptz_operation_id] ON [gb_ptz_operation] ([operation_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'uk_ptz_operation_idempotency')
    CREATE UNIQUE INDEX [uk_ptz_operation_idempotency] ON [gb_ptz_operation] ([channel_id], [idempotency_key]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_channel_time')
    CREATE INDEX [idx_ptz_operation_channel_time] ON [gb_ptz_operation] ([channel_id], [created_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_channel_cmd_id')
    CREATE INDEX [idx_ptz_operation_channel_cmd_id] ON [gb_ptz_operation] ([channel_id], [cmd_type], [id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_device_sn')
    CREATE INDEX [idx_ptz_operation_device_sn] ON [gb_ptz_operation] ([device_id], [sn]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_status_time')
    CREATE INDEX [idx_ptz_operation_status_time] ON [gb_ptz_operation] ([status], [created_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_status_next_attempt')
    CREATE INDEX [idx_ptz_operation_status_next_attempt] ON [gb_ptz_operation] ([status], [next_attempt_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_status_queue_deadline')
    CREATE INDEX [idx_ptz_operation_status_queue_deadline] ON [gb_ptz_operation] ([status], [queue_deadline_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_status_transport_deadline')
    CREATE INDEX [idx_ptz_operation_status_transport_deadline] ON [gb_ptz_operation] ([status], [transport_deadline_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_status_deadline')
    CREATE INDEX [idx_ptz_operation_status_deadline] ON [gb_ptz_operation] ([status], [deadline_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_call_id')
    CREATE INDEX [idx_ptz_operation_call_id] ON [gb_ptz_operation] ([call_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_state') AND name = N'uk_ptz_state_channel')
    CREATE UNIQUE INDEX [uk_ptz_state_channel] ON [gb_ptz_state] ([channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_state') AND name = N'idx_ptz_state_device')
    CREATE INDEX [idx_ptz_state_device] ON [gb_ptz_state] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_state') AND name = N'idx_ptz_state_received')
    CREATE INDEX [idx_ptz_state_received] ON [gb_ptz_state] ([received_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_preset') AND name = N'uk_ptz_preset_channel_number')
    CREATE UNIQUE INDEX [uk_ptz_preset_channel_number] ON [gb_ptz_preset] ([channel_id], [preset_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_preset') AND name = N'idx_ptz_preset_device')
    CREATE INDEX [idx_ptz_preset_device] ON [gb_ptz_preset] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_cruise_track') AND name = N'uk_ptz_cruise_channel_track')
    CREATE UNIQUE INDEX [uk_ptz_cruise_channel_track] ON [gb_ptz_cruise_track] ([channel_id], [track_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_cruise_track') AND name = N'idx_ptz_cruise_device')
    CREATE INDEX [idx_ptz_cruise_device] ON [gb_ptz_cruise_track] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation_attempt') AND name = N'uk_ptz_operation_attempt')
    CREATE UNIQUE INDEX [uk_ptz_operation_attempt] ON [gb_ptz_operation_attempt] ([operation_id], [attempt_no]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation_attempt') AND name = N'idx_ptz_attempt_status_lease')
    CREATE INDEX [idx_ptz_attempt_status_lease] ON [gb_ptz_operation_attempt] ([status], [lease_until]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_home_position') AND name = N'uk_ptz_home_position_channel')
    CREATE UNIQUE INDEX [uk_ptz_home_position_channel] ON [gb_ptz_home_position] ([channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_home_position') AND name = N'idx_ptz_home_position_device')
    CREATE INDEX [idx_ptz_home_position_device] ON [gb_ptz_home_position] ([device_id]);

UPDATE [gb_ptz_operation]
SET [status] = 'unknown',
    [error_code] = 'TRANSPORT_UNKNOWN',
    [error_message] = COALESCE(NULLIF([error_message], ''), 'legacy home-position operation upgraded without response metadata'),
    [completed_at] = COALESCE([completed_at], CURRENT_TIMESTAMP)
WHERE [action] IN ('home_position', 'refresh_home_position')
  AND [status] IN ('queued', 'sent')
  AND [response_required] = 0
  AND [completed_at] IS NULL;
