-- 回滚 2026-10-18-hide-plugin-menus：把「插件示例」「插件管理」重新显示在侧边栏。
UPDATE sys_menu SET hide=0 WHERE path='/demo' AND parent_id=0 AND deleted_at IS NULL;
UPDATE sys_menu SET hide=0 WHERE path='/plugins/example' AND parent_id=140247 AND deleted_at IS NULL;
UPDATE sys_menu SET hide=0 WHERE path='/system/pluginsmanager' AND parent_id=10 AND deleted_at IS NULL;
