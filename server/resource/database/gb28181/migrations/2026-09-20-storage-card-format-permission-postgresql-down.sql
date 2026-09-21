-- 回退 2026-09-20-storage-card-format-permission-postgresql.sql —— PostgreSQL 方言
--
-- ⛔ 软删除语义、删除顺序与理由见 MySQL 版本（-down.sql）的注释。

DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND v2='POST';

DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST');

DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:device:format_sd');

UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST' AND deleted_at IS NULL;

UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:device:format_sd' AND deleted_at IS NULL;
