-- zlm-single-menu-workbench:down:start
-- Restore the direct ZLM navigation introduced by V3 (MySQL).
UPDATE `sys_menu` SET `redirect`='/gb28181/zlm/overview',`component`='',`title`='流媒体管理',`icon`='lucide:Clapperboard',`type`=1,`hide`=0,`updated_at`=NOW() WHERE `path`='/media' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW() WHERE `path` IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling','/media/nodes/:id') AND `deleted_at` IS NULL;
SET @media_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/media' AND `deleted_at` IS NULL);
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ClusterOverview',`title`='集群总览',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/overview' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/NodeList',`title`='节点管理',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/nodes' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/RuntimeOverview',`title`='总览',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/runtime' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/StreamManagement',`title`='流管理',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/streams' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SessionManagement',`title`='会话管理',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/sessions' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ProxyManagement',`title`='拉流/推流代理',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/proxies' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/FFmpegSources',`title`='FFmpeg 源',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/ffmpeg-sources' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/RTPServices',`title`='RTP 服务',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/rtp-servers' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/ServerConfig',`title`='服务器配置',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/config' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SchedulerStrategy',`title`='调度策略',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/scheduler' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=@media_menu_id,`component`='gb28181/zlm/SchedulerLog',`title`='调度日志',`hide`=0,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/scheduler/logs' AND `deleted_at` IS NULL;
UPDATE `sys_menu` SET `parent_id`=0,`component`='gb28181/zlm/NodeDetail',`title`='节点详情',`hide`=1,`updated_at`=NOW() WHERE `path`='/gb28181/zlm/nodes/:id' AND `deleted_at` IS NULL;
-- zlm-single-menu-workbench:down:end
