-- Work recording permissions (MySQL 5.7+, idempotent).
-- Start and stop are separate buttons; both may read status and job detail.
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`icon`,`created_at`,`updated_at`,`created_by`)
SELECT COALESCE((SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/multi-screen-playback' AND `type` IN (1,2) AND `deleted_at` IS NULL),0),'','Permission_gb28181_work_recording_start','','开启作业录像',1,0,100,3,'gb28181:work-recording:start','',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:work-recording:start' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`icon`,`created_at`,`updated_at`,`created_by`)
SELECT COALESCE((SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/multi-screen-playback' AND `type` IN (1,2) AND `deleted_at` IS NULL),0),'','Permission_gb28181_work_recording_stop','','停止作业录像',1,0,100,3,'gb28181:work-recording:stop','',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:work-recording:stop' AND `deleted_at` IS NULL);

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m
WHERE m.`permission` IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=m.`id`);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT s.title,s.path,s.method,'GB28181 作业录像',NOW(),NOW(),1 FROM (
 SELECT '开启作业录像' AS title,'/api/gb28181/work-recordings' AS path,'POST' AS method
 UNION ALL SELECT '停止作业录像','/api/gb28181/work-recordings/:id/stop','POST'
 UNION ALL SELECT '查询作业录像状态','/api/gb28181/work-recordings/status','GET'
 UNION ALL SELECT '查询作业录像详情','/api/gb28181/work-recordings/:id','GET'
) s
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=s.path AND a.`method`=s.method AND a.`deleted_at` IS NULL);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`deleted_at` IS NULL
WHERE m.`permission`='gb28181:work-recording:start' AND m.`deleted_at` IS NULL
  AND ((a.`path`='/api/gb28181/work-recordings' AND a.`method`='POST')
    OR (a.`path` IN ('/api/gb28181/work-recordings/status','/api/gb28181/work-recordings/:id') AND a.`method`='GET'))
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`deleted_at` IS NULL
WHERE m.`permission`='gb28181:work-recording:stop' AND m.`deleted_at` IS NULL
  AND ((a.`path`='/api/gb28181/work-recordings/:id/stop' AND a.`method`='POST')
    OR (a.`path` IN ('/api/gb28181/work-recordings/status','/api/gb28181/work-recordings/:id') AND a.`method`='GET'))
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT DISTINCT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id`
JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE m.`permission` IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
