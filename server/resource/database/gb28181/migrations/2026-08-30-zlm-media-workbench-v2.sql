-- media-workbench-v2:start
-- Flatten media management into six visible workspaces while preserving legacy permission anchors (MySQL).
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT 0,'/media','Media','','gb28181/zlm/workbench/MediaEntry','流媒体管理','lucide:Clapperboard',9,1,'',0,1,NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media' AND `deleted_at` IS NULL);
UPDATE `sys_menu` SET `parent_id`=0,`name`='Media',`redirect`='',`component`='gb28181/zlm/workbench/MediaEntry',`title`='流媒体管理',`icon`='lucide:Clapperboard',`sort`=9,`type`=1,`permission`='',`hide`=0,`keep_alive`=1,`updated_at`=NOW()
WHERE `path`='/media' AND `deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/overview','media-overview','','gb28181/zlm/workbench/MediaOverview','媒体总览','lucide:LayoutDashboard',10,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/overview' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-overview',m.`redirect`='',m.`component`='gb28181/zlm/workbench/MediaOverview',m.`title`='媒体总览',m.`icon`='lucide:LayoutDashboard',m.`sort`=10,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/overview' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/monitoring','media-monitoring','','gb28181/zlm/workbench/MediaMonitoring','媒体监控','lucide:Activity',20,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/monitoring' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-monitoring',m.`redirect`='',m.`component`='gb28181/zlm/workbench/MediaMonitoring',m.`title`='媒体监控',m.`icon`='lucide:Activity',m.`sort`=20,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/monitoring' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/ingress','media-ingress','','gb28181/zlm/workbench/IngressManagement','接入管理','lucide:RadioTower',30,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/ingress' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-ingress',m.`redirect`='',m.`component`='gb28181/zlm/workbench/IngressManagement',m.`title`='接入管理',m.`icon`='lucide:RadioTower',m.`sort`=30,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/ingress' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/recordings','media-recordings','','gb28181/zlm/workbench/RecordingCenter','录制中心','lucide:Cloud',40,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/recordings' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-recordings',m.`redirect`='',m.`component`='gb28181/zlm/workbench/RecordingCenter',m.`title`='录制中心',m.`icon`='lucide:Cloud',m.`sort`=40,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/recordings' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/nodes','media-nodes','','gb28181/zlm/workbench/NodeManagement','节点管理','lucide:Server',50,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/nodes' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-nodes',m.`redirect`='',m.`component`='gb28181/zlm/workbench/NodeManagement',m.`title`='节点管理',m.`icon`='lucide:Server',m.`sort`=50,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/nodes' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/scheduling','media-scheduling','','gb28181/zlm/workbench/SchedulingManagement','调度管理','lucide:Workflow',60,2,'',0,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/scheduling' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-scheduling',m.`redirect`='',m.`component`='gb28181/zlm/workbench/SchedulingManagement',m.`title`='调度管理',m.`icon`='lucide:Workflow',m.`sort`=60,m.`type`=2,m.`permission`='',m.`hide`=0,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/scheduling' AND m.`deleted_at` IS NULL;

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`redirect`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`keep_alive`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/media/nodes/:id','media-node-detail','','gb28181/zlm/workbench/nodes/NodeDetail','节点详情','lucide:Server',99,2,'',1,1,NOW(),NOW(),1 FROM `sys_menu` p
WHERE p.`path`='/media/nodes' AND p.`deleted_at` IS NULL
AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/media/nodes/:id' AND `deleted_at` IS NULL);
UPDATE `sys_menu` m JOIN `sys_menu` p ON p.`path`='/media/nodes' AND p.`deleted_at` IS NULL
SET m.`parent_id`=p.`id`,m.`name`='media-node-detail',m.`redirect`='',m.`component`='gb28181/zlm/workbench/nodes/NodeDetail',m.`title`='节点详情',m.`icon`='lucide:Server',m.`sort`=99,m.`type`=2,m.`permission`='',m.`hide`=1,m.`keep_alive`=1,m.`updated_at`=NOW()
WHERE m.`path`='/media/nodes/:id' AND m.`deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/overview' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/runtime' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/streams' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/sessions' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/proxies' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/ffmpeg-sources' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/rtp-servers' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/recording-schedules' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/nodes' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/nodes/:id' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/config' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/scheduler' AND `deleted_at` IS NULL;

UPDATE `sys_menu` SET `component`='gb28181/zlm/workbench/LegacyMediaRoute',`hide`=1,`updated_at`=NOW()
WHERE `path`='/gb28181/zlm/scheduler/logs' AND `deleted_at` IS NULL;

-- workspace-role-union:/media
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/overview
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/overview' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/overview')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/monitoring
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/monitoring' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/ingress
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/ingress' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/recordings
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/recordings' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/cloud-recordings','/gb28181/recording-schedules')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/nodes
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/nodes' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/nodes/:id
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/nodes/:id' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- workspace-role-union:/media/scheduling
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`,target.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` source ON source.`id`=rm.`menu_id` AND source.`deleted_at` IS NULL
JOIN `sys_menu` target ON target.`path`='/media/scheduling' AND target.`deleted_at` IS NULL
WHERE source.`path` IN ('/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=rm.`role_id` AND existing.`menu_id`=target.`id`);

-- media-workbench-v2:end
