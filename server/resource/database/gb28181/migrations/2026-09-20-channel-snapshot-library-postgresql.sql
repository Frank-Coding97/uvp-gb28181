-- 抓拍图像库：设备/平台抓拍产出的**图片文件事实表**
-- —— PostgreSQL 方言，与 2026-09-20-channel-snapshot-library.sql 等价。
--
-- 背景见 docs/snapshot-flow-design.md §2.4。建表 + 读接口权限（gb28181:device:snapshot）。
-- 与 gb_channel.snapshot_url 的分工见 MySQL 版本注释（那个是"最近一张"，本表是全部历史）。

CREATE TABLE IF NOT EXISTS gb_channel_snapshot (
    id BIGSERIAL,
    device_id BIGINT NOT NULL DEFAULT 0,
    channel_id BIGINT NOT NULL DEFAULT 0,
    channel_code VARCHAR(20) NOT NULL,
    session_id VARCHAR(64),
    file_name VARCHAR(64) NOT NULL,
    rel_path VARCHAR(255) NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    md5 VARCHAR(32),
    captured_at TIMESTAMP(3) NOT NULL,
    source VARCHAR(16) NOT NULL,
    created_by BIGINT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    deleted_at TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT uk_channel_snapshot_file UNIQUE (channel_code, file_name)
);

CREATE INDEX IF NOT EXISTS idx_channel_snapshot_channel_time ON gb_channel_snapshot (channel_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_channel_snapshot_session ON gb_channel_snapshot (session_id);
CREATE INDEX IF NOT EXISTS idx_channel_snapshot_captured ON gb_channel_snapshot (captured_at);

-- channel-snapshot-library-permissions:start
-- ---- 读接口：GET /api/gb28181/device-mgmt/snapshots/:id/content ----
-- ⛔ 这是**稳定**读接口（按库里的行 id 取图，凭证是 JWT）：与上传那条
--    `/device-snapshots/uploads/:token/:filename` 是两条路，后者的凭证是会话 token 会过期。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '读取抓拍图像','/api/gb28181/device-mgmt/snapshots/:id/content','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots/:id/content' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots/:id/content' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots/:id/content' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1='/api/gb28181/device-mgmt/snapshots/:id/content' AND p.v2='GET' AND p.v3='*');

-- ---- 列表接口：GET /api/gb28181/device-mgmt/snapshots ----
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询抓拍图像库','/api/gb28181/device-mgmt/snapshots','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_' || rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/device-mgmt/snapshots' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0='role_' || rm.role_id AND p.v1='/api/gb28181/device-mgmt/snapshots' AND p.v2='GET' AND p.v3='*');

-- ---- 图像库一级菜单：/gb28181/snapshot-library ----
-- component 必须等于 web/src/views 下组件文件的相对路径（去掉 views/ 与 .vue），见 MySQL 版本注释。
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'/gb28181/snapshot-library','snapshot-library','gb28181/snapshot-library/index','图像库',0,0,55,2,'','lucide:Images',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/snapshot-library' AND deleted_at IS NULL);

-- ① 内置管理员（role 1，硬编码；不查 sys_role，理由见 MySQL 版本注释）。
INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.path='/gb28181/snapshot-library' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

-- ② 已持有抓拍按钮的角色（⛔ JOIN 带 type=3，别把同 permission 的目录行当按钮）。
INSERT INTO sys_role_menu(role_id,menu_id)
SELECT DISTINCT rm.role_id,lib.id
FROM sys_role_menu rm
JOIN sys_menu btn ON btn.id=rm.menu_id AND btn.deleted_at IS NULL
  AND btn.permission='gb28181:device:snapshot' AND btn.type=3
CROSS JOIN sys_menu lib
WHERE lib.path='/gb28181/snapshot-library' AND lib.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=lib.id);
-- channel-snapshot-library-permissions:end
