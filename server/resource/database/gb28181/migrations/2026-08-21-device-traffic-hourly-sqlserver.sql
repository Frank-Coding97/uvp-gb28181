IF OBJECT_ID(N'gb_device_traffic_hourly',N'U') IS NULL BEGIN
  CREATE TABLE [gb_device_traffic_hourly] (
    [id] BIGINT IDENTITY(1,1) NOT NULL CONSTRAINT [pk_device_traffic_hourly] PRIMARY KEY,
    [stat_hour] DATETIME2(6) NOT NULL,
    [device_code] VARCHAR(64) NOT NULL,
    [channel_code] VARCHAR(64) NOT NULL CONSTRAINT [df_traffic_hourly_channel] DEFAULT '',
    [owner_dept_id] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_owner] DEFAULT 0,
    [upstream_bytes] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_up_bytes] DEFAULT 0,
    [downstream_bytes] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_down_bytes] DEFAULT 0,
    [upstream_duration_seconds] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_up_duration] DEFAULT 0,
    [downstream_duration_seconds] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_down_duration] DEFAULT 0,
    [upstream_sessions] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_up_sessions] DEFAULT 0,
    [downstream_sessions] BIGINT NOT NULL CONSTRAINT [df_traffic_hourly_down_sessions] DEFAULT 0,
    [created_at] DATETIME2(3) NULL,
    [updated_at] DATETIME2(3) NULL,
    CONSTRAINT [uk_traffic_hourly_scope] UNIQUE ([stat_hour],[device_code],[channel_code])
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_hourly') AND name=N'idx_traffic_hourly_device')
  CREATE INDEX [idx_traffic_hourly_device] ON [gb_device_traffic_hourly] ([device_code],[stat_hour]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_hourly') AND name=N'idx_traffic_hourly_owner_dept')
  CREATE INDEX [idx_traffic_hourly_owner_dept] ON [gb_device_traffic_hourly] ([owner_dept_id]);
