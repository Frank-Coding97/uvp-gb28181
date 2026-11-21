IF OBJECT_ID(N'gb_cascade_alarm_delivery', N'U') IS NULL
  CREATE TABLE [gb_cascade_alarm_delivery] (
    [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    [delivery_key] NVARCHAR(64) NOT NULL CONSTRAINT [uk_cascade_alarm_delivery_key] UNIQUE,
    [platform_id] BIGINT NOT NULL,
    [source_device_code] NVARCHAR(20) NOT NULL,
    [source_resource_code] NVARCHAR(20) NOT NULL,
    [published_code] NVARCHAR(20) NOT NULL,
    [profile_version] NVARCHAR(16) NOT NULL,
    [binding_fingerprint] NVARCHAR(64) NOT NULL,
    [payload] VARBINARY(MAX) NOT NULL,
    [notify_targets] NVARCHAR(MAX) NULL,
    [state] NVARCHAR(16) NOT NULL CONSTRAINT [df_cascade_alarm_delivery_state] DEFAULT N'pending',
    [attempts] INT NOT NULL CONSTRAINT [df_cascade_alarm_delivery_attempts] DEFAULT 0,
    [next_attempt_at] DATETIME2(3) NOT NULL,
    [lease_owner] NVARCHAR(64) NOT NULL CONSTRAINT [df_cascade_alarm_delivery_owner] DEFAULT N'',
    [lease_until] DATETIME2(3) NULL,
    [last_error] NVARCHAR(MAX) NULL,
    [sent_at] DATETIME2(3) NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL
  );
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_cascade_alarm_delivery') AND name=N'idx_cascade_alarm_delivery_due')
  CREATE INDEX [idx_cascade_alarm_delivery_due] ON [gb_cascade_alarm_delivery] ([platform_id], [state], [next_attempt_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_cascade_alarm_delivery') AND name=N'idx_cascade_alarm_delivery_lease')
  CREATE INDEX [idx_cascade_alarm_delivery_lease] ON [gb_cascade_alarm_delivery] ([lease_until]);
