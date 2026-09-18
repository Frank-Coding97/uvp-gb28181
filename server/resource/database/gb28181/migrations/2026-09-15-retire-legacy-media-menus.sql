-- retire-legacy-media-menus:start
-- 「流媒体管理」(/media,id=140355)下并存三代菜单:12 条 /gb28181/zlm/* 兼容跳转壳、
-- 1 条同样误配成跳转壳的 /media/recordings,以及 5 条 /media/* 正典工作台。
-- 跳转壳自身零子菜单、零写入权限(按钮权限 140411-140478 已全部挂在 /media/* 下),
-- 仅因库里 hide 由迁移设计的 1 漂移成 0 才在侧栏渲染出 19 条子菜单。
-- 本次物理删除这 13 条跳转壳,并把只挂在它们身上的 19 个媒体读接口(393-411)
-- 按语义重定向到对应正典工作台,避免接口失去菜单归属。
-- 接口运行时鉴权走 sys_casbin_rule(本次不动);sys_menu_api 只是「菜单-接口」归属展示。
INSERT IGNORE INTO `sys_menu_api` (`menu_id`,`api_id`) SELECT DISTINCT tgt.`id`, ma.`api_id` FROM `sys_menu_api` ma JOIN `sys_menu` src ON src.`id`=ma.`menu_id` AND src.`component`='gb28181/zlm/workbench/LegacyMediaRoute' JOIN `sys_menu` tgt ON tgt.`deleted_at` IS NULL AND tgt.`path`=CASE src.`path` WHEN '/gb28181/zlm/overview' THEN '/media/overview' WHEN '/gb28181/zlm/runtime' THEN '/media/monitoring' WHEN '/gb28181/zlm/streams' THEN '/media/monitoring' WHEN '/gb28181/zlm/sessions' THEN '/media/monitoring' WHEN '/gb28181/zlm/proxies' THEN '/media/ingress' WHEN '/gb28181/zlm/ffmpeg-sources' THEN '/media/ingress' WHEN '/gb28181/zlm/rtp-servers' THEN '/media/ingress' WHEN '/gb28181/zlm/nodes' THEN '/media/nodes' WHEN '/gb28181/zlm/nodes/:id' THEN '/media/nodes' WHEN '/gb28181/zlm/config' THEN '/media/nodes' WHEN '/gb28181/zlm/scheduler' THEN '/media/scheduling' WHEN '/gb28181/zlm/scheduler/logs' THEN '/media/scheduling' ELSE NULL END;
DELETE ma FROM `sys_menu_api` ma JOIN `sys_menu` m ON m.`id`=ma.`menu_id` WHERE m.`component`='gb28181/zlm/workbench/LegacyMediaRoute';
DELETE rm FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` WHERE m.`component`='gb28181/zlm/workbench/LegacyMediaRoute';
DELETE FROM `sys_menu` WHERE `component`='gb28181/zlm/workbench/LegacyMediaRoute';
-- 实时日志控制台是平台级日志流,不属于流媒体;移到系统管理下,与 /system/login-log 并列。
UPDATE `sys_menu` SET `parent_id`=10,`path`='/system/realtime-log',`sort`=2,`updated_at`=NOW() WHERE `path`='/gb28181/realtime-log' AND `deleted_at` IS NULL;
-- retire-legacy-media-menus:end
