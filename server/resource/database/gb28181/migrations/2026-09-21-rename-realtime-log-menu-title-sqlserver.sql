-- 将「实时日志控制台」更名为「运行日志」。
-- 理由：与「日志中心」下同级菜单（SIP 日志 / 登录日志 / 操作日志 / 定时任务日志）保持「X日志」同构；
-- 「实时」无区分度，「控制台」描述展示形式而非内容；该页镜像后端进程 stdout。
-- 定位用权限码而非 path：菜单 path 已由 2026-09-15 迁移从 /gb28181/realtime-log 移至 /system/realtime-log。
-- 幂等：SQL Server。
UPDATE sys_menu
SET title=N'运行日志', updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:log:view' AND deleted_at IS NULL AND title<>N'运行日志';
