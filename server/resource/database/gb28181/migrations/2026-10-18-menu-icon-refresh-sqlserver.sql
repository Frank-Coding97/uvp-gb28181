-- 「设备权限工作台」和「OpenAPI 客户端」原来都写 lucide:KeyRound，侧边栏出现两把一模一样的钥匙。
-- 本次只换图标，不动 sort / parent_id / 授权：
--   设备权限工作台 → lucide:ShieldCheck（授权/权限管控）
--   OpenAPI 客户端  → lucide:Plug（第三方系统接入）
-- ⛔ 这两个图标名必须同时存在于前端白名单 web/src/utils/lucide-menu-icons.ts，
--    否则 getLucideIconComponent 返回 undefined，菜单图标静默不渲染（2026-10-04 踩过）。
UPDATE sys_menu SET icon='lucide:ShieldCheck' WHERE path='/gb28181/device-assignment' AND parent_id=0 AND deleted_at IS NULL;
UPDATE sys_menu SET icon='lucide:Plug' WHERE path='/gb28181/openapi-client' AND parent_id=0 AND deleted_at IS NULL;
