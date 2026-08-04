-- Alarm management menu, permissions, APIs and query index (MySQL 5.7+).
-- Repeatable; view access follows existing device-management roles.

SET @schema_name := DATABASE();
SET @device_menu_id := (
  SELECT `id` FROM `sys_menu`
  WHERE `path` IN ('/gb28181/device-mgmt/index', '/gb28181/device-mgmt')
    AND `deleted_at` IS NULL
  ORDER BY CASE WHEN `path`='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END, `id`
  LIMIT 1
);
SET @gb_parent_menu_id := (SELECT `parent_id` FROM `sys_menu` WHERE `id`=@device_menu_id);

SET @sql := IF(
  (SELECT COUNT(*) FROM information_schema.statistics
   WHERE table_schema=@schema_name AND table_name='gb_alarm_event' AND index_name='idx_alarm_time')=0,
  'CREATE INDEX `idx_alarm_time` ON `gb_alarm_event` (`alarm_time`)',
  'SELECT 1'
);
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

UPDATE `sys_menu` SET
  `parent_id`=IFNULL(@gb_parent_menu_id, `parent_id`),
  `name`='gb28181-alarm-management',
  `component`='gb28181/alarm-management/index',
  `title`='告警管理', `icon`='lucide:BellRing', `type`=2,
  `permission`='gb28181:alarm:view', `hide`=0, `disable`=0,
  `updated_at`=NOW(), `deleted_at`=NULL
WHERE `path`='/gb28181/alarm-management';

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`disable`,`created_at`,`updated_at`,`created_by`)
SELECT @gb_parent_menu_id,'/gb28181/alarm-management','gb28181-alarm-management','gb28181/alarm-management/index','告警管理','lucide:BellRing',30,2,'gb28181:alarm:view',0,0,NOW(),NOW(),1
WHERE @gb_parent_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/gb28181/alarm-management' AND `deleted_at` IS NULL);

SET @alarm_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/alarm-management' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`type`,`permission`,`hide`,`created_at`,`updated_at`,`created_by`)
SELECT @alarm_menu_id,'','','','物理删除告警',3,'gb28181:alarm:delete',1,NOW(),NOW(),1
WHERE @alarm_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:alarm:delete' AND `deleted_at` IS NULL);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 告警管理',NOW(),NOW(),1
FROM (
  SELECT '查询告警列表' title,'/api/gb28181/alarms' path,'GET' method
  UNION ALL SELECT '查询告警详情','/api/gb28181/alarms/:id','GET'
  UNION ALL SELECT '物理删除单条告警','/api/gb28181/alarms/:id','DELETE'
  UNION ALL SELECT '批量物理删除告警','/api/gb28181/alarms/batch-delete','POST'
) v
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT source_role.`role_id`,@alarm_menu_id FROM `sys_role_menu` source_role
WHERE source_role.`menu_id`=@device_menu_id AND @alarm_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` existing WHERE existing.`role_id`=source_role.`role_id` AND existing.`menu_id`=@alarm_menu_id);

SET @delete_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `permission`='gb28181:alarm:delete' AND `deleted_at` IS NULL);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,@delete_menu_id WHERE @delete_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id`=1 AND `menu_id`=@delete_menu_id);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @alarm_menu_id,a.`id` FROM `sys_api` a
WHERE a.`path` IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id') AND a.`method`='GET'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@alarm_menu_id AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @delete_menu_id,a.`id` FROM `sys_api` a
WHERE ((a.`path`='/api/gb28181/alarms/:id' AND a.`method`='DELETE')
    OR (a.`path`='/api/gb28181/alarms/batch-delete' AND a.`method`='POST'))
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@delete_menu_id AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm JOIN `sys_api` a
WHERE rm.`menu_id`=@alarm_menu_id
  AND a.`path` IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id') AND a.`method`='GET'
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE ((a.`path`='/api/gb28181/alarms/:id' AND a.`method`='DELETE')
    OR (a.`path`='/api/gb28181/alarms/batch-delete' AND a.`method`='POST'))
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
