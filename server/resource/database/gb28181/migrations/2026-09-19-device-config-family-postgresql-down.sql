-- 回退 2026-09-19-device-config-family-postgresql.sql —— PostgreSQL 方言
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行。
-- ⚠️ 回滚会丢弃已回读的设备配置快照；它们可由下次 ConfigDownload 重建，不是业务凭证。

DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/device-configs' AND v2 IN ('GET','POST');
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND method IN ('GET','POST'));
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/device-configs' AND method IN ('GET','POST') AND deleted_at IS NULL;

DROP TABLE IF EXISTS gb_device_config;
