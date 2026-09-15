-- 重排「一级菜单」（parent_id=0）的侧栏展示顺序，统一 sort 值（MySQL 方言）。
--
-- 依据：后端 models.SysMenuList.TreeSort（server/app/models/sysmenu.go:140）把 sort=0
--   视为「未设置」并沉到最底部（同为 0 时按 id 升序），非 0 值才按升序排在前面。
--   历史上 2026-06-30 ~ 09-01 的多个迁移反复改过一级菜单 sort，导致：
--     · /media 与 /gb28181/device-assignment 撞号（都拿到 9）—— sort.Slice 是不稳定排序,
--       相等 sort 的先后完全不可控；
--     · 值域 1 → 99 中间大片空档；
--     · 语义相近的项被拆散（SIP 三兄弟在 6/7/8，国标级联却在 13；录像三项掉到 35/36/99）。
--   本条按用户指定的目标顺序重排，并改用 10 的倍数留出插队空档。
--
-- 定位一律用 path：menu_id 由各环境自增分配，跨环境会漂移
--   （同一菜单在 mysql / postgresql / sqlserver 基线里的 id 并不相同）。
--
-- 注意：/gb28181/openapi-client 属在途功能（codex/aksk-* 分支族，尚未合入 develop），
--   此处只调整它的排序值，不涉及菜单行、接口绑定、casbin 或任何功能开关。
--
-- 目标顺序（自上而下，用户指定）：
--   首页 10 / 设备列表 20 / 多屏播放 30 / 告警管理 40 /
--   云端录像 50 / 录像计划 60 / 国标级联 70 /
--   SIP 接入信息 80 / SIP 日志 90 / 国标服务配置 100 / 国标接入安全 110 /
--   设备权限工作台 120 / OpenAPI 客户端 130 / 流媒体管理 140 /
--   系统管理 150 / 任务调度 160 / 插件示例 170
--   设备录像回放 180（hide=1，侧栏不显示，仅在保持 sort 唯一时占位）
UPDATE `sys_menu` SET `sort`=10, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/home' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>10);
UPDATE `sys_menu` SET `sort`=20, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path` IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>20);
UPDATE `sys_menu` SET `sort`=30, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/multi-screen-playback' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>30);
UPDATE `sys_menu` SET `sort`=40, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/alarm-management' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>40);
UPDATE `sys_menu` SET `sort`=50, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>50);
UPDATE `sys_menu` SET `sort`=60, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/recording-schedules' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>60);
UPDATE `sys_menu` SET `sort`=70, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/cascade' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>70);
UPDATE `sys_menu` SET `sort`=80, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/sip/platform' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>80);
UPDATE `sys_menu` SET `sort`=90, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/sip-traces' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>90);
UPDATE `sys_menu` SET `sort`=100, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/sip/config' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>100);
UPDATE `sys_menu` SET `sort`=110, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/security-preview' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>110);
UPDATE `sys_menu` SET `sort`=120, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/device-assignment' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>120);
UPDATE `sys_menu` SET `sort`=130, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/openapi-client' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>130);
UPDATE `sys_menu` SET `sort`=140, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/media' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>140);
UPDATE `sys_menu` SET `sort`=150, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/system' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>150);
UPDATE `sys_menu` SET `sort`=160, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/sysjobs' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>160);
UPDATE `sys_menu` SET `sort`=170, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/demo' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>170);
UPDATE `sys_menu` SET `sort`=180, `updated_at`=NOW() WHERE (`parent_id`=0 OR `parent_id` IS NULL) AND `path`='/gb28181/device-record-playback/:channelId' AND `deleted_at` IS NULL AND (`sort` IS NULL OR `sort`<>180);
