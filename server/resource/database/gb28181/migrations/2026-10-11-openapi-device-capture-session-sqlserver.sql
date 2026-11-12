IF OBJECT_ID(N'dbo.sys_openapi_device_capture_session', N'U') IS NULL
BEGIN
  CREATE TABLE [dbo].[sys_openapi_device_capture_session] (
    [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    [client_id] BIGINT NOT NULL,
    [idempotency_digest] NVARCHAR(64) NOT NULL,
    [request_digest] NVARCHAR(64) NOT NULL,
    [session_id] NVARCHAR(36) NOT NULL,
    [registry_session_id] NVARCHAR(36) NOT NULL DEFAULT N'',
    [operation_id] NVARCHAR(64) NOT NULL DEFAULT N'',
    [device_id] NVARCHAR(20) NOT NULL,
    [channel_id] NVARCHAR(20) NOT NULL,
    [internal_device_id] BIGINT NOT NULL,
    [internal_channel_id] BIGINT NOT NULL,
    [department_id] BIGINT NOT NULL,
    [snap_num] INT NOT NULL,
    [interval_seconds] INT NOT NULL,
    [state] NVARCHAR(24) NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [uk_openapi_capture_idem] UNIQUE ([client_id], [idempotency_digest]),
    CONSTRAINT [uk_openapi_capture_session] UNIQUE ([session_id])
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_capture_session') AND name=N'idx_openapi_capture_registry_session')
  CREATE INDEX [idx_openapi_capture_registry_session] ON [dbo].[sys_openapi_device_capture_session] ([registry_session_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_capture_session') AND name=N'idx_openapi_capture_session_client')
  CREATE INDEX [idx_openapi_capture_session_client] ON [dbo].[sys_openapi_device_capture_session] ([client_id], [device_id], [channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'dbo.sys_openapi_device_capture_session') AND name=N'idx_openapi_capture_state_updated')
  CREATE INDEX [idx_openapi_capture_state_updated] ON [dbo].[sys_openapi_device_capture_session] ([state], [created_at]);
