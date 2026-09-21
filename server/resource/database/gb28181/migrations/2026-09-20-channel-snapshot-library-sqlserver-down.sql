-- 回退「抓拍图像库」（SQL Server 方言，与 2026-09-20-channel-snapshot-library-down.sql 等价）。
-- ⛔ 软删 sys_api，不物理删；⚠️ 图片文件本身留在磁盘上，不随本回滚被删（见 MySQL down 注释）。

DELETE FROM sys_casbin_rule WHERE v1=N'/api/gb28181/device-mgmt/snapshots/:id/content' AND v2=N'GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/snapshots/:id/content' AND method=N'GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path=N'/api/gb28181/device-mgmt/snapshots/:id/content' AND method=N'GET' AND deleted_at IS NULL;

-- 列表接口（两条路径不通配，各删各的）。
DELETE FROM sys_casbin_rule WHERE v1=N'/api/gb28181/device-mgmt/snapshots' AND v2=N'GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path=N'/api/gb28181/device-mgmt/snapshots' AND method=N'GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path=N'/api/gb28181/device-mgmt/snapshots' AND method=N'GET' AND deleted_at IS NULL;

-- 图像库一级菜单与授权（先删授权、再软删菜单；理由见 MySQL down 注释）。
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path=N'/gb28181/snapshot-library');
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path=N'/gb28181/snapshot-library' AND deleted_at IS NULL;

IF OBJECT_ID(N'gb_channel_snapshot', N'U') IS NOT NULL DROP TABLE [gb_channel_snapshot];
