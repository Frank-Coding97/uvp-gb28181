-- 补齐「运行日志」菜单图标（MySQL，幂等）。
-- 背景：该菜单 2026-09-10 建行时未写 icon，2026-09-17 建「日志中心」目录时也只给目录设了图标，
-- 同组 SIP 日志 / 登录日志 / 操作日志 / 定时任务日志 的 icon 均由各自建菜单迁移带上，唯独它为空。
-- 前端 menu-item-icon.vue 是三级回落（lucide icon → svg_icon → icon 组件），
-- icon 为空 ⇒ 三条全落空 ⇒ 图标区整块不渲染，且不报错（静默无图标）。
-- 取 lucide:Terminal：该页镜像后端进程 stdout（页面本体即终端），
-- 与同组 FileText / FileClock / History 语义区分开。
-- ⛔ 按 permission 定位而非 path：菜单 path 已被 2026-09-15 迁移改过。
UPDATE `sys_menu` SET `icon`='lucide:Terminal',`updated_at`=NOW()
WHERE `permission`='gb28181:log:view' AND `deleted_at` IS NULL
  AND (`icon` IS NULL OR `icon`<>'lucide:Terminal');
