-- 回退「目标跟踪」（GB/T 28181-2022 A.2.3.1.14，SQL Server）：软删 API 与绑定，再删掉落库表。
--
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行 ——
--    物理删会让"这条 API 曾经存在过"这段历史消失，而 sys_menu_api /
--    sys_casbin_rule 残留行会变成指向不存在 API 的孤儿规则。
--
-- ⛔ 本迁移**没有**新建 sys_menu 权限点（读/写分别复用既有的 gb28181:ptz:view /
--    gb28181:ptz:control），所以 down 里**不能**去删那两个 sys_menu 行 ——
--    它们服务着一整族云台接口，删掉等于顺手废掉平台所有云台读写。

DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/target-track';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/target-track' AND deleted_at IS NULL;

DROP TABLE IF EXISTS [gb_device_target_track];
