-- 「插件示例」和「插件管理」是开发期的脚手架功能，正式交付时不应该出现在左侧导航里。
-- 2026-10-10 那次是直接从全量脚本里删行（commit 1fda1a01），但 10-13 基线改从开发库重派生
-- 之后这两条又被带回来了，于是它们重新出现在侧边栏 ⇒ 本次改成 hide=1（不删除，路由仍可直达）：
--   /demo（插件示例，一级目录）· /plugins/example（示例列表页）· /system/pluginsmanager（系统管理下的插件管理）
-- ⛔ 只动 hide，不动 parent_id / sort / permission / 授权关系：隐藏后接口权限、casbin 全都不受影响，
--    以后要开放只需把这些行的 hide 改回 0。
UPDATE sys_menu SET hide=1 WHERE path='/demo' AND parent_id=0 AND deleted_at IS NULL;
UPDATE sys_menu SET hide=1 WHERE path='/plugins/example' AND parent_id=140247 AND deleted_at IS NULL;
UPDATE sys_menu SET hide=1 WHERE path='/system/pluginsmanager' AND parent_id=10 AND deleted_at IS NULL;
