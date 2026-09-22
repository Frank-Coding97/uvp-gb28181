-- 将「实时日志控制台」更名为「运行日志」。
-- 理由：该菜单位于「日志中心」下，同级为 SIP 日志 / 登录日志 / 操作日志 / 定时任务日志，
-- 命名应为「X日志」同构；「实时」在同级四个同样实时可查的菜单前没有区分度，
-- 「控制台」描述的是展示形式（终端 UI）而非内容。该页镜像的是后端进程 stdout，
-- 「运行日志」与数据源一致。
-- 定位用权限码而非 path：菜单 path 已由 2026-09-15 迁移
-- 从 /gb28181/realtime-log 移至 /system/realtime-log。
-- 幂等：MySQL。
UPDATE `sys_menu`
SET `title`='运行日志', `updated_at`=NOW()
WHERE `permission`='gb28181:log:view' AND `deleted_at` IS NULL AND `title`<>'运行日志';
