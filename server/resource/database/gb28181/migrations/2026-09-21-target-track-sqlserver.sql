-- GB/T 28181-2022 A.2.3.1.14「目标跟踪控制命令」（SQL Server）。
-- 与 MySQL 版逐字对应，只换方言；语义说明见 2026-09-21-target-track.sql。

-- target-track:start

IF OBJECT_ID(N'gb_device_target_track', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_target_track] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL CONSTRAINT [df_target_track_channel] DEFAULT 0,
        [target_code] NVARCHAR(20) NOT NULL,
        [mode] NVARCHAR(16) NOT NULL,
        [device_id2] NVARCHAR(20) NOT NULL CONSTRAINT [df_target_track_device_id2] DEFAULT N'',
        [area_length] INT,
        [area_width] INT,
        [area_mid_point_x] INT,
        [area_mid_point_y] INT,
        [area_length_x] INT,
        [area_length_y] INT,
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_target_track_source_seq] DEFAULT 0,
        [source_sn] INT NOT NULL CONSTRAINT [df_target_track_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64),
        [commanded_by] BIGINT NOT NULL CONSTRAINT [df_target_track_commanded_by] DEFAULT 0,
        [commanded_by_dept_id] BIGINT NOT NULL CONSTRAINT [df_target_track_commanded_dept] DEFAULT 0,
        [commanded_at] DATETIME2(3) NOT NULL,
        [raw_summary] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_target_track] PRIMARY KEY ([id]),
        CONSTRAINT [uk_target_track_target] UNIQUE ([device_id], [target_code])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_target_track') AND name = N'idx_target_track_device')
    CREATE INDEX [idx_target_track_device] ON [gb_device_target_track] ([device_id], [commanded_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_target_track') AND name = N'idx_target_track_channel')
    CREATE INDEX [idx_target_track_channel] ON [gb_device_target_track] ([channel_id]);

-- target-track:end

-- target-track-permissions:start

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'读取目标跟踪已下发指令',N'/api/gb28181/device-mgmt/channel/:id/target-track',N'GET',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND method=N'GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'下发目标跟踪',N'/api/gb28181/device-mgmt/channel/:id/target-track',N'POST',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND method=N'POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),N'/api/gb28181/device-mgmt/channel/:id/target-track',N'GET',N'*',N'',N''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2=N'GET' AND p.v3=N'*');

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),N'/api/gb28181/device-mgmt/channel/:id/target-track',N'POST',N'*',N'',N''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=N'/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2=N'POST' AND p.v3=N'*');

-- target-track-permissions:end
