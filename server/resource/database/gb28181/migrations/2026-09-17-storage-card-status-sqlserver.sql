-- GB/T 28181-2022 A.2.4.14 / A.2.6.16「存储卡状态查询」。
-- 1) 落库表 gb_device_storage_card：每张卡一行（见 models.GbDeviceStorageCard）
-- 2) 查询接口 /channel/:id/storage-cards 的权限绑定，沿用 gb28181:ptz:view
--    （与既有 device-status / cruise-tracks 同族：都是"看设备事实"的读接口）
IF OBJECT_ID(N'gb_device_storage_card', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_storage_card] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [target_code] NVARCHAR(20) NOT NULL,
        [card_id] INT NOT NULL,
        [hdd_name] NVARCHAR(128) NOT NULL CONSTRAINT [df_storage_card_hdd_name] DEFAULT N'',
        [status] NVARCHAR(16) NOT NULL CONSTRAINT [df_storage_card_status] DEFAULT N'unknown',
        [format_progress] INT,
        [capacity_mb] INT NOT NULL CONSTRAINT [df_storage_card_capacity] DEFAULT 0,
        [free_space_mb] INT NOT NULL CONSTRAINT [df_storage_card_free_space] DEFAULT 0,
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_storage_card_source_seq] DEFAULT 0,
        [source_sn] INT NOT NULL CONSTRAINT [df_storage_card_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64),
        [observed_at] DATETIME2(3) NOT NULL,
        [raw_summary] NVARCHAR(MAX),
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_storage_card] PRIMARY KEY ([id]),
        CONSTRAINT [uk_storage_card_target] UNIQUE ([device_id], [target_code], [card_id])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_storage_card') AND name = N'uk_storage_card_target')
    CREATE UNIQUE INDEX [uk_storage_card_target] ON [gb_device_storage_card] ([device_id], [target_code], [card_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_storage_card') AND name = N'idx_storage_card_device')
    CREATE INDEX [idx_storage_card_device] ON [gb_device_storage_card] ([device_id], [observed_at]);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查询存储卡状态',N'/api/gb28181/device-mgmt/channel/:id/storage-cards',N'GET',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/channel/:id/storage-cards' AND method=N'GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/storage-cards' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path=N'/api/gb28181/device-mgmt/channel/:id/storage-cards' AND a.method=N'GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
