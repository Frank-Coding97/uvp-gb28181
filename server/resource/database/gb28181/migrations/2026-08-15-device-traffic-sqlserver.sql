-- Device/channel traffic accounting and runtime-monitor permissions (SQL Server 2017+).
IF OBJECT_ID(N'gb_device_traffic_session',N'U') IS NULL BEGIN
  CREATE TABLE [gb_device_traffic_session] (
    [id] BIGINT IDENTITY(1,1) NOT NULL CONSTRAINT [pk_device_traffic_session] PRIMARY KEY,
    [business_key] VARCHAR(255) NOT NULL, [node_id] BIGINT NOT NULL, [media_server_uuid] VARCHAR(128) NOT NULL DEFAULT '',
    [zlm_session_id] VARCHAR(128) NOT NULL DEFAULT '', [direction] VARCHAR(16) NOT NULL, [device_code] VARCHAR(64) NOT NULL,
    [channel_code] VARCHAR(64) NOT NULL DEFAULT '', [owner_dept_id] BIGINT NOT NULL DEFAULT 0, [media_kind] VARCHAR(32) NOT NULL DEFAULT '',
    [schema] VARCHAR(32) NOT NULL DEFAULT '', [vhost] VARCHAR(128) NOT NULL DEFAULT '', [app] VARCHAR(64) NOT NULL DEFAULT '',
    [stream] VARCHAR(255) NOT NULL DEFAULT '', [create_stamp] BIGINT NOT NULL DEFAULT 0, [last_total_bytes] BIGINT NOT NULL DEFAULT 0,
    [settled_total_bytes] BIGINT NOT NULL DEFAULT 0, [duration_seconds] BIGINT NOT NULL DEFAULT 0, [state] VARCHAR(16) NOT NULL,
    [started_at] DATETIME2(6) NULL, [last_seen_at] DATETIME2(6) NULL, [ended_at] DATETIME2(6) NULL,
    [unattributed_reason] VARCHAR(255) NOT NULL DEFAULT '', [created_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    [updated_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT [uk_traffic_session_business] UNIQUE ([business_key])
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_node_state') CREATE INDEX [idx_traffic_session_node_state] ON [gb_device_traffic_session] ([node_id],[state]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_zlm') CREATE INDEX [idx_traffic_session_zlm] ON [gb_device_traffic_session] ([zlm_session_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_direction') CREATE INDEX [idx_traffic_session_direction] ON [gb_device_traffic_session] ([direction]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_device_started') CREATE INDEX [idx_traffic_session_device_started] ON [gb_device_traffic_session] ([device_code],[channel_code],[started_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_channel_started') CREATE INDEX [idx_traffic_session_channel_started] ON [gb_device_traffic_session] ([channel_code],[started_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_session') AND name=N'idx_traffic_session_owner_dept') CREATE INDEX [idx_traffic_session_owner_dept] ON [gb_device_traffic_session] ([owner_dept_id]);

IF OBJECT_ID(N'gb_device_traffic_daily',N'U') IS NULL BEGIN
  CREATE TABLE [gb_device_traffic_daily] (
    [id] BIGINT IDENTITY(1,1) NOT NULL CONSTRAINT [pk_device_traffic_daily] PRIMARY KEY, [stat_date] DATE NOT NULL,
    [device_code] VARCHAR(64) NOT NULL, [channel_code] VARCHAR(64) NOT NULL DEFAULT '', [owner_dept_id] BIGINT NOT NULL DEFAULT 0,
    [upstream_bytes] BIGINT NOT NULL DEFAULT 0, [downstream_bytes] BIGINT NOT NULL DEFAULT 0,
    [upstream_duration_seconds] BIGINT NOT NULL DEFAULT 0, [downstream_duration_seconds] BIGINT NOT NULL DEFAULT 0,
    [upstream_sessions] BIGINT NOT NULL DEFAULT 0, [downstream_sessions] BIGINT NOT NULL DEFAULT 0,
    [created_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, [updated_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT [uk_traffic_daily_scope] UNIQUE ([stat_date],[device_code],[channel_code])
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_daily') AND name=N'idx_traffic_daily_device') CREATE INDEX [idx_traffic_daily_device] ON [gb_device_traffic_daily] ([device_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_daily') AND name=N'idx_traffic_daily_owner_dept') CREATE INDEX [idx_traffic_daily_owner_dept] ON [gb_device_traffic_daily] ([owner_dept_id]);

IF OBJECT_ID(N'gb_device_traffic_gap',N'U') IS NULL BEGIN
  CREATE TABLE [gb_device_traffic_gap] (
    [id] BIGINT IDENTITY(1,1) NOT NULL CONSTRAINT [pk_device_traffic_gap] PRIMARY KEY, [node_id] BIGINT NOT NULL,
    [reason] VARCHAR(32) NOT NULL, [state] VARCHAR(16) NOT NULL, [started_at] DATETIME2(6) NOT NULL, [ended_at] DATETIME2(6) NULL,
    [detail] VARCHAR(500) NOT NULL DEFAULT '', [created_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    [updated_at] DATETIME2(6) NOT NULL DEFAULT CURRENT_TIMESTAMP
  );
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_gap') AND name=N'idx_traffic_gap_node_state') CREATE INDEX [idx_traffic_gap_node_state] ON [gb_device_traffic_gap] ([node_id],[reason],[state]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_device_traffic_gap') AND name=N'idx_traffic_gap_started') CREATE INDEX [idx_traffic_gap_started] ON [gb_device_traffic_gap] ([started_at]);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT dm.[id],'','','',N'查看运行监控',3,'gb28181:traffic:view',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM [sys_menu] dm WHERE dm.[id]=(SELECT TOP 1 [id] FROM [sys_menu] WHERE [path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND [deleted_at] IS NULL ORDER BY CASE WHEN [path]='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,[id])
  AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:traffic:view' AND [deleted_at] IS NULL);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 运行监控',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 (N'查询流量汇总','/api/gb28181/device-traffic/summary','GET'),(N'查询流量趋势','/api/gb28181/device-traffic/trend','GET'),
 (N'查询实时流量','/api/gb28181/device-traffic/realtime','GET'),(N'查询流量会话','/api/gb28181/device-traffic/sessions','GET'),
 (N'查询统计覆盖率','/api/gb28181/device-traffic/coverage','GET'),(N'查询当前观看','/api/gb28181/device-traffic/viewers','GET'),
 (N'强退观看连接','/api/gb28181/device-traffic/viewers/kick','POST')
) v(title,path,method) WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT rm.[role_id],tm.[id] FROM [sys_role_menu] rm JOIN [sys_menu] dm ON dm.[id]=rm.[menu_id] CROSS JOIN [sys_menu] tm
WHERE dm.[path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.[deleted_at] IS NULL
  AND tm.[permission]='gb28181:traffic:view' AND tm.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=rm.[role_id] AND x.[menu_id]=tm.[id]);
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:traffic:view' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[permission]='gb28181:traffic:view' AND m.[deleted_at] IS NULL AND a.[path] LIKE '/api/gb28181/device-traffic/%' AND a.[method]='GET' AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_' + CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id] CROSS JOIN [sys_api] a
WHERE m.[permission]='gb28181:traffic:view' AND a.[path] LIKE '/api/gb28181/device-traffic/%' AND a.[method]='GET' AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_' + CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a
WHERE a.[path]='/api/gb28181/device-traffic/viewers/kick' AND a.[method]='POST' AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
