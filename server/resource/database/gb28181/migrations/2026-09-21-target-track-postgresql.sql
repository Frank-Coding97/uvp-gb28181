-- GB/T 28181-2022 A.2.3.1.14「目标跟踪控制命令」（PostgreSQL）。
-- 与 MySQL 版逐字对应，只换方言；语义说明见 2026-09-21-target-track.sql。

-- target-track:start

CREATE TABLE IF NOT EXISTS gb_device_target_track (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL DEFAULT 0,
    target_code VARCHAR(20) NOT NULL,
    mode VARCHAR(16) NOT NULL,
    device_id2 VARCHAR(20) NOT NULL DEFAULT '',
    area_length INTEGER,
    area_width INTEGER,
    area_mid_point_x INTEGER,
    area_mid_point_y INTEGER,
    area_length_x INTEGER,
    area_length_y INTEGER,
    source_operation_seq BIGINT NOT NULL DEFAULT 0,
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    commanded_by BIGINT NOT NULL DEFAULT 0,
    commanded_by_dept_id BIGINT NOT NULL DEFAULT 0,
    commanded_at TIMESTAMP(3) NOT NULL,
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_target_track_target UNIQUE (device_id, target_code)
);

CREATE INDEX IF NOT EXISTS idx_target_track_device ON gb_device_target_track (device_id, commanded_at);
CREATE INDEX IF NOT EXISTS idx_target_track_channel ON gb_device_target_track (channel_id);

-- target-track:end

-- target-track-permissions:start

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取目标跟踪已下发指令','/api/gb28181/device-mgmt/channel/:id/target-track','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '下发目标跟踪','/api/gb28181/device-mgmt/channel/:id/target-track','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,'/api/gb28181/device-mgmt/channel/:id/target-track','GET','*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1='/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2='GET' AND p.v3='*');

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,'/api/gb28181/device-mgmt/channel/:id/target-track','POST','*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/channel/:id/target-track' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1='/api/gb28181/device-mgmt/channel/:id/target-track' AND p.v2='POST' AND p.v3='*');

-- target-track-permissions:end
