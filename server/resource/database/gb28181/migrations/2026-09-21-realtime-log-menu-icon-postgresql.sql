-- 补齐「运行日志」菜单图标。
-- 背景：该菜单建行时未写 icon，同组其余四项的 icon 均由各自建菜单迁移带上，唯独它为空。
-- 前端渲染是三级回落（lucide icon → svg_icon → icon 组件），icon 为空 ⇒ 图标区整块不渲染且不报错。
-- 取 lucide:Terminal：该页镜像后端进程 stdout（页面本体即终端）。
-- ⛔ 按 permission 定位而非 path：菜单 path 已被 2026-09-15 迁移改过。
-- 幂等：PostgreSQL。
UPDATE sys_menu SET icon='lucide:Terminal', updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:log:view' AND deleted_at IS NULL
  AND (icon IS NULL OR icon<>'lucide:Terminal');
