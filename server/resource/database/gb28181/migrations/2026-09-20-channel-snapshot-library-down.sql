-- 回退「抓拍图像库」：软删读接口权限，再删落库表。
--
-- ⛔ 与既有 down 一致使用**软删除**语义（deleted_at）回收 sys_api，不物理删 ——
--    物理删会让"这条 API 曾经存在过"这段历史消失，而 sys_menu_api / sys_casbin_rule
--    的残留行会变成指向不存在 API 的孤儿规则。
-- ⚠️ 回滚会丢弃图像库的**索引**（哪张图属于哪个通道/会话/什么时刻拍的）。
--    **图片文件本身留在磁盘上**（`<serverroot 父目录>/gb-device-snapshots/…`），
--    不会随本回滚被删 —— 它们是设备侧已经产生的原始凭证，只能人工处理。
--    重装本迁移**无法**自动重建这些行：库行里没有"文件在哪"以外的信息，
--    而文件扫描不在本迁移的职责里。

DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/snapshots/:id/content' AND v2='GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots/:id/content' AND method='GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/snapshots/:id/content' AND method='GET' AND deleted_at IS NULL;

-- 列表接口（与读图接口分开回收：两条路径不通配，各删各的）。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/snapshots' AND v2='GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/device-mgmt/snapshots' AND method='GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/device-mgmt/snapshots' AND method='GET' AND deleted_at IS NULL;

-- 图像库一级菜单与角色授权。
-- ⛔ 顺序：**先删授权、再软删菜单**。反过来的话按 `deleted_at IS NULL` 已经查不到菜单行，
--    授权行就留成指向"看不见的菜单"的孤儿（而且再 up 会新增一行菜单、孤儿仍指着旧行）。
-- ⛔ 菜单行用**软删**（与 sys_api 同语义：保留"这个入口曾经存在过"），
--    授权行物理删（sys_role_menu 没有 deleted_at 列）。
-- ⚠️ 子查询里刻意**不加** `deleted_at IS NULL`：多轮 up/down 会留下多条同 path 的历史行，
--    它们上面的授权同样必须清掉，否则有孤儿。
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/snapshot-library');
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/snapshot-library' AND deleted_at IS NULL;

DROP TABLE IF EXISTS `gb_channel_snapshot`;
