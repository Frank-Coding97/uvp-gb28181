-- 一级菜单排序整理：
--   ① 把「告警管理」「图像库」挪到「国标级联」之后；
--   ② 把「固件仓库」从第 3 位移到「OpenAPI 客户端」之后（与设备/接入类菜单分开，靠后放置）；
--   ③ 把一级菜单 sort 统一成 10 的倍数（原先是 21/52/55/145 这类零散值，
--      后续再插菜单只能靠猜空位，且「告警管理」排在了「多屏播放」前面）。
-- 只改 sort，不动 parent_id / path / permission / 授权关系。
-- 定位键用 path：菜单 id 是基线常量，但 path 才是跨版本稳定的契约。
-- 目标顺序（即 sort 升序）：
--   首页 / 设备列表 / 多屏播放 / 云端录像 / 录像缓存 / 录像计划 /
--   国标级联 / 告警管理 / 图像库 / SIP 接入信息 / 国标服务配置 / 国标接入安全 /
--   设备权限工作台 / OpenAPI 客户端 / 固件仓库 / 流媒体管理 / 日志中心 /
--   系统管理 / 定时任务 / 插件示例（隐藏）/ 设备录像回放（隐藏页）

UPDATE sys_menu SET sort=10 WHERE path='/home' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=20 WHERE path='/gb28181/device-mgmt/index' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=30 WHERE path='/gb28181/multi-screen-playback' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=40 WHERE path='/gb28181/cloud-recordings' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=50 WHERE path='/gb28181/record-cache' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=60 WHERE path='/gb28181/recording-schedules' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=70 WHERE path='/gb28181/cascade' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=80 WHERE path='/gb28181/alarm-management' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=90 WHERE path='/gb28181/snapshot-library' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=100 WHERE path='/gb28181/sip/platform' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=110 WHERE path='/gb28181/sip/config' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=120 WHERE path='/security-preview' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=130 WHERE path='/gb28181/device-assignment' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=140 WHERE path='/gb28181/openapi-client' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=150 WHERE path='/gb28181/firmware-repo/index' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=160 WHERE path='/media' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=170 WHERE path='/log-center' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=180 WHERE path='/system' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=190 WHERE path='/sysjobs' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=200 WHERE path='/demo' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=210 WHERE path='/gb28181/device-record-playback/:channelId' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
