-- 回滚 2026-10-18-menu-icon-refresh：两个菜单图标还原成原来的 lucide:KeyRound。
UPDATE sys_menu SET icon='lucide:KeyRound' WHERE path='/gb28181/device-assignment' AND parent_id=0 AND deleted_at IS NULL;
UPDATE sys_menu SET icon='lucide:KeyRound' WHERE path='/gb28181/openapi-client' AND parent_id=0 AND deleted_at IS NULL;
