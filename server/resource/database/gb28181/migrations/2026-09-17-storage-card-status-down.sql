-- 回退「存储卡状态查询」：软删 API 与菜单绑定，再删掉落库表。
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at），不是物理删 sys_api 行 ——
--    物理删会让"这条 API 曾经存在过"这段历史消失，而 sys_menu_api /
--    sys_casbin_rule 残留行会变成指向不存在 API 的孤儿规则。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND v2='GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND method='GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards' AND method='GET' AND deleted_at IS NULL;

DROP TABLE IF EXISTS `gb_device_storage_card`;
