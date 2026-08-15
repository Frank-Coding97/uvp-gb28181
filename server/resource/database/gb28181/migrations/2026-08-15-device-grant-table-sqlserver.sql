-- 设备共享授权表(SQL Server 方言,幂等)
IF OBJECT_ID(N'gb_device_grant', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_grant] (
        [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
        [device_id] INT NOT NULL,
        [target_type] VARCHAR(16) NOT NULL CONSTRAINT [df_device_grant_target_type] DEFAULT '',
        [target_id] INT NOT NULL CONSTRAINT [df_device_grant_target_id] DEFAULT 0,
        [created_by] INT CONSTRAINT [df_device_grant_created_by] DEFAULT 0,
        [created_at] DATETIME NULL,
        [updated_at] DATETIME NULL,
        [deleted_at] DATETIME NULL
    );
    CREATE UNIQUE INDEX [uk_device_target] ON [gb_device_grant] ([device_id], [target_type], [target_id]);
    CREATE INDEX [idx_target] ON [gb_device_grant] ([target_type], [target_id]);
    CREATE INDEX [idx_device] ON [gb_device_grant] ([device_id]);
    CREATE INDEX [idx_deleted_at] ON [gb_device_grant] ([deleted_at]);
END;
