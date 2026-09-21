-- 回退 2026-09-20-storage-card-format-permission.sql —— MySQL 方言
--
-- ⛔ 与既有 down 一致使用**软删除**语义（sys_api / sys_menu 走 deleted_at），
--    不是物理删 —— 物理删会让"这个权限点曾经存在过"这段历史消失，而
--    sys_menu_api / sys_role_menu / sys_casbin_rule 残留行会变成指向不存在物件的孤儿规则。
-- ⚠️ 回滚后已授权的角色立即失去格式化能力（casbin 规则被物理删）。这是预期语义。
--
-- 删除顺序（有依赖）：casbin → menu_api → role_menu → 软删 sys_api / sys_menu。

DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND v2='POST';

DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST');

DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='gb28181:device:format_sd');

UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/channel/:id/storage-cards/format' AND method='POST' AND deleted_at IS NULL;

UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:device:format_sd' AND deleted_at IS NULL;
