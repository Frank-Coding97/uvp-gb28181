-- Remove alarm management configuration without deleting gb_alarm_event data.
DELETE FROM `sys_casbin_rule` WHERE `v1` IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete');
DELETE ma FROM `sys_menu_api` ma JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE a.`path` IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete');
DELETE rm FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
WHERE m.`path`='/gb28181/alarm-management' OR m.`permission` IN ('gb28181:alarm:view','gb28181:alarm:delete');
DELETE FROM `sys_menu` WHERE `path`='/gb28181/alarm-management' OR `permission` IN ('gb28181:alarm:view','gb28181:alarm:delete');
DELETE FROM `sys_api` WHERE `path` IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete');
SET @schema_name := DATABASE();
SET @sql := IF((SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=@schema_name AND table_name='gb_alarm_event' AND index_name='idx_alarm_time')=1,
  'DROP INDEX `idx_alarm_time` ON `gb_alarm_event`','SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
