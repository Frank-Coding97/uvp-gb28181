IF OBJECT_ID(N'dbo.sys_openapi_device_subscription', N'U') IS NULL
BEGIN
  CREATE TABLE [dbo].[sys_openapi_device_subscription] (
    [id] NVARCHAR(36) NOT NULL PRIMARY KEY,
    [client_id] BIGINT NOT NULL,
    [device_id] BIGINT NOT NULL,
    [kind] NVARCHAR(32) NOT NULL,
    [enabled] BIT NOT NULL DEFAULT 1,
    [expires_seconds] INT NOT NULL DEFAULT 3600,
    [expires_at] DATETIME2(3) NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [uk_openapi_device_subscription] UNIQUE ([client_id], [device_id], [kind])
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_subscription') AND name=N'idx_openapi_device_sub_client')
  CREATE INDEX [idx_openapi_device_sub_client] ON [dbo].[sys_openapi_device_subscription] ([client_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_subscription') AND name=N'idx_openapi_device_sub_enabled')
  CREATE INDEX [idx_openapi_device_sub_enabled] ON [dbo].[sys_openapi_device_subscription] ([enabled]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_subscription') AND name=N'idx_openapi_device_sub_expires')
  CREATE INDEX [idx_openapi_device_sub_expires] ON [dbo].[sys_openapi_device_subscription] ([expires_at]);
