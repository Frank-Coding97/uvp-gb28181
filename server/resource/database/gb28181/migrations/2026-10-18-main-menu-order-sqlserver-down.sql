-- 回滚 2026-10-18-main-menu-order：把一级菜单 sort 还原成整理前的取值。
-- 用途：升级包回退到旧版本时，让菜单顺序与旧版本一致（顺序本身不影响任何功能）。
-- 原顺序：home 10 / 设备列表 20 / 固件仓库 21 / 多屏播放 30 / 告警管理 40 /
--   云端录像 50 / 录像缓存 52 / 图像库 55 / 录像计划 60 / 国标级联 70 /
--   SIP 接入信息 80 / 国标服务配置 100 / 国标接入安全 110 / 设备权限工作台 120 /
--   OpenAPI 客户端 130 / 流媒体管理 140 / 日志中心 145 / system 150 /
--   sysjobs 160 / 插件示例 170 / 设备录像回放 180

UPDATE sys_menu SET sort=10 WHERE path='/home' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=20 WHERE path='/gb28181/device-mgmt/index' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=30 WHERE path='/gb28181/multi-screen-playback' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=50 WHERE path='/gb28181/cloud-recordings' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=52 WHERE path='/gb28181/record-cache' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=60 WHERE path='/gb28181/recording-schedules' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=70 WHERE path='/gb28181/cascade' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=40 WHERE path='/gb28181/alarm-management' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=55 WHERE path='/gb28181/snapshot-library' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=80 WHERE path='/gb28181/sip/platform' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=100 WHERE path='/gb28181/sip/config' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=110 WHERE path='/security-preview' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=120 WHERE path='/gb28181/device-assignment' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=130 WHERE path='/gb28181/openapi-client' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=21 WHERE path='/gb28181/firmware-repo/index' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=140 WHERE path='/media' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=145 WHERE path='/log-center' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=150 WHERE path='/system' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=160 WHERE path='/sysjobs' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=170 WHERE path='/demo' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
UPDATE sys_menu SET sort=180 WHERE path='/gb28181/device-record-playback/:channelId' AND parent_id=0 AND type IN (1,2) AND deleted_at IS NULL;
