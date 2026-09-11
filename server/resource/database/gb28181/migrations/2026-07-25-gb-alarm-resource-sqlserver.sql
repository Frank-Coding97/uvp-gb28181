-- 2026-07-25: persist GB/T 28181 alarm input/output Catalog resources.
-- SQL Server 2017+; repeatable migration.

IF OBJECT_ID(N'gb_alarm_resource', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_alarm_resource] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [owner_dept_id] BIGINT NOT NULL,
    [device_id] BIGINT NOT NULL CONSTRAINT [df_alarm_resource_device_id] DEFAULT 0,
    [device_code] NVARCHAR(20) NOT NULL,
    [alarm_code] NVARCHAR(20) NOT NULL,
    [resource_type] NVARCHAR(16) NOT NULL,
    [type_code] NVARCHAR(3) NOT NULL,
    [name] NVARCHAR(255) NOT NULL,
    [raw_parent_ids] NVARCHAR(512) NOT NULL CONSTRAINT [df_alarm_resource_parents] DEFAULT N'',
    [status] SMALLINT NOT NULL CONSTRAINT [df_alarm_resource_status] DEFAULT 0,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    [deleted_at] DATETIME2(3) NULL,
    CONSTRAINT [pk_gb_alarm_resource] PRIMARY KEY ([id]),
    CONSTRAINT [uk_alarm_resource_code] UNIQUE ([owner_dept_id], [device_code], [alarm_code])
  );
END;

IF OBJECT_ID(N'gb_alarm_resource_parent', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_alarm_resource_parent] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [alarm_resource_id] BIGINT NOT NULL,
    [parent_code] NVARCHAR(20) NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_alarm_resource_parent] PRIMARY KEY ([id]),
    CONSTRAINT [uk_alarm_resource_parent] UNIQUE ([alarm_resource_id], [parent_code])
  );
END;

IF OBJECT_ID(N'gb_alarm_binding', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_alarm_binding] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [channel_code] NVARCHAR(20) NOT NULL,
    [alarm_resource_id] BIGINT NOT NULL,
    [source] NVARCHAR(16) NOT NULL CONSTRAINT [df_alarm_binding_source] DEFAULT N'manual',
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_alarm_binding] PRIMARY KEY ([id]),
    CONSTRAINT [uk_alarm_binding_channel] UNIQUE ([device_id], [channel_code])
  );
END;

IF OBJECT_ID(N'gb_catalog_node', N'U') IS NOT NULL
   AND COL_LENGTH(N'gb_catalog_node', N'alarm_resource_id') IS NULL
  ALTER TABLE [gb_catalog_node] ADD [alarm_resource_id] BIGINT NULL;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_device')
  CREATE INDEX [idx_alarm_resource_device] ON [gb_alarm_resource] ([owner_dept_id], [device_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_device_id')
  CREATE INDEX [idx_alarm_resource_device_id] ON [gb_alarm_resource] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_alarm_code')
  CREATE INDEX [idx_alarm_resource_alarm_code] ON [gb_alarm_resource] ([alarm_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_type')
  CREATE INDEX [idx_alarm_resource_type] ON [gb_alarm_resource] ([resource_type]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_deleted_at')
  CREATE INDEX [idx_alarm_resource_deleted_at] ON [gb_alarm_resource] ([deleted_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource_parent') AND name = N'idx_alarm_parent_resource')
  CREATE INDEX [idx_alarm_parent_resource] ON [gb_alarm_resource_parent] ([alarm_resource_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource_parent') AND name = N'idx_alarm_parent_code')
  CREATE INDEX [idx_alarm_parent_code] ON [gb_alarm_resource_parent] ([parent_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_binding') AND name = N'idx_alarm_binding_device')
  CREATE INDEX [idx_alarm_binding_device] ON [gb_alarm_binding] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_binding') AND name = N'idx_alarm_binding_resource')
  CREATE INDEX [idx_alarm_binding_resource] ON [gb_alarm_binding] ([alarm_resource_id]);
IF OBJECT_ID(N'gb_catalog_node', N'U') IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_catalog_node') AND name = N'idx_catalog_alarm_resource')
  CREATE INDEX [idx_catalog_alarm_resource] ON [gb_catalog_node] ([alarm_resource_id]);
