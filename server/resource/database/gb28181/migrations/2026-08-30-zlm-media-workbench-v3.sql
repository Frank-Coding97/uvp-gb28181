-- zlm-admin-parity-v3:start
-- Restore zlm-admin-style direct pages and keep GB28181 recordings independent (MySQL).
SET @media_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/media' AND `deleted_at` IS NULL);
UPDATE `sys_menu` SET `redirect`='/gb28181/zlm/overview',`component`='',`title`='流媒体管理',`icon`='lucide:Clapperboard',`hide`=0,`updated_at`=NOW() WHERE `path`='/media' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ClusterOverview',`title`='集群总览',`icon`='lucide:LayoutDashboard',`sort`=10,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/overview' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/NodeList',`title`='节点管理',`icon`='lucide:Server',`sort`=20,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/nodes' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/RuntimeOverview',`title`='总览',`icon`='lucide:Gauge',`sort`=30,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/runtime' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/StreamManagement',`title`='流管理',`icon`='lucide:RadioTower',`sort`=40,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/streams' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SessionManagement',`title`='会话管理',`icon`='lucide:Users',`sort`=50,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/sessions' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ProxyManagement',`title`='拉流/推流代理',`icon`='lucide:Network',`sort`=60,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/proxies' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/FFmpegSources',`title`='FFmpeg 源',`icon`='lucide:Clapperboard',`sort`=70,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/ffmpeg-sources' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/RTPServices',`title`='RTP 服务',`icon`='lucide:Waypoints',`sort`=80,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/rtp-servers' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ServerConfig',`title`='服务器配置',`icon`='lucide:Settings2',`sort`=90,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/config' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SchedulerStrategy',`title`='调度策略',`icon`='lucide:Workflow',`sort`=100,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/scheduler' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SchedulerLog',`title`='调度日志',`icon`='lucide:History',`sort`=110,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/scheduler/logs' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=0,`component`='gb28181/zlm/NodeDetail',`title`='节点详情',`hide`=1,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/nodes/:id' AND `deleted_at` IS NULL;

SET @device_menu_id := (SELECT `id` FROM `sys_menu` WHERE `path` IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND `deleted_at` IS NULL ORDER BY CASE WHEN `path`='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,`id` LIMIT 1);
SET @gb_parent_menu_id := (SELECT `parent_id` FROM `sys_menu` WHERE `id`=@device_menu_id);
UPDATE `sys_menu` SET `parent_id`=@gb_parent_menu_id,`component`='gb28181/cloud-recordings/index',`title`='云端录像',`icon`='lucide:Cloud',`sort`=35,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@gb_parent_menu_id,`component`='gb28181/recording-schedules/index',`title`='录像计划',`icon`='lucide:CalendarClock',`sort`=36,`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/recording-schedules' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW() WHERE `path` IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND `deleted_at` IS NULL;
-- zlm-admin-parity-v3:end
