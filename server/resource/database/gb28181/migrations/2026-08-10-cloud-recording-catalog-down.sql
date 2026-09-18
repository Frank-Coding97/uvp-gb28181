DELETE FROM `sys_casbin_rule` WHERE `v1` LIKE '/api/gb28181/cloud-recordings/%';
DELETE ma FROM `sys_menu_api` ma JOIN `sys_api` a ON a.`id`=ma.`api_id` WHERE a.`path` LIKE '/api/gb28181/cloud-recordings/%';
DELETE rm FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` WHERE m.`path`='/gb28181/cloud-recordings' OR m.`permission` IN ('gb28181:recording:view','gb28181:recording:reconcile');
DELETE FROM `sys_menu` WHERE `path`='/gb28181/cloud-recordings' OR `permission` IN ('gb28181:recording:view','gb28181:recording:reconcile');
DELETE FROM `sys_api` WHERE `path` LIKE '/api/gb28181/cloud-recordings/%';

DROP TABLE IF EXISTS `gb_recording_reconcile_state`;

UPDATE `gb_recording_file` SET `start_time`=COALESCE(`start_time`,'1970-01-01 00:00:00'),`time_len`=COALESCE(`time_len`,0),`file_size`=COALESCE(`file_size`,0);
ALTER TABLE `gb_recording_file`
  MODIFY COLUMN `start_time` datetime NOT NULL,
  MODIFY COLUMN `time_len` decimal(12,3) NOT NULL DEFAULT '0.000',
  MODIFY COLUMN `file_size` bigint unsigned NOT NULL DEFAULT '0';

ALTER TABLE `gb_recording_file`
  DROP INDEX `uk_recording_file_key`,
  DROP INDEX `idx_recording_file_date_tuple`,
  DROP INDEX `idx_recording_file_missing`,
  ADD UNIQUE KEY `uk_recording_file_node_path` (`node_id`,`file_path`(766));

ALTER TABLE `gb_recording_file`
  DROP COLUMN `updated_at`,
  DROP COLUMN `reconcile_miss_count`,
  DROP COLUMN `missing_at`,
  DROP COLUMN `last_seen_at`,
  DROP COLUMN `discovered_at`,
  DROP COLUMN `record_date`,
  DROP COLUMN `metadata_state`,
  DROP COLUMN `source`,
  DROP COLUMN `file_key`,
  DROP COLUMN `owner_dept_id`,
  DROP COLUMN `device_name`,
  DROP COLUMN `channel_name`,
  DROP COLUMN `channel_code`;
