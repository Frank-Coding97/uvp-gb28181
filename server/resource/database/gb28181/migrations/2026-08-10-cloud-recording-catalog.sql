-- Cloud recording catalog schema (MySQL 5.7+).
-- Preserve existing file facts; the application SHA-256 vector is used for the backfill.

ALTER TABLE `gb_recording_file`
  ADD COLUMN `channel_code` varchar(20) NOT NULL DEFAULT '' COMMENT '通道国标编码快照',
  ADD COLUMN `channel_name` varchar(255) NOT NULL DEFAULT '' COMMENT '通道名称快照',
  ADD COLUMN `device_name` varchar(255) NOT NULL DEFAULT '' COMMENT '设备名称快照',
  ADD COLUMN `owner_dept_id` bigint unsigned NOT NULL DEFAULT '0' COMMENT '归属部门快照',
  ADD COLUMN `file_key` varchar(64) NULL COMMENT '节点和完整路径 SHA-256',
  ADD COLUMN `source` varchar(16) NOT NULL DEFAULT 'hook' COMMENT 'hook/reconcile',
  ADD COLUMN `metadata_state` varchar(16) NOT NULL DEFAULT 'complete' COMMENT 'complete/partial',
  ADD COLUMN `record_date` date NULL COMMENT 'ZLM 目录日期',
  ADD COLUMN `discovered_at` datetime NULL COMMENT '首次进入目录时间',
  ADD COLUMN `last_seen_at` datetime NULL COMMENT '最近确认存在时间',
  ADD COLUMN `missing_at` datetime NULL COMMENT '确认缺失时间',
  ADD COLUMN `reconcile_miss_count` int NOT NULL DEFAULT '0' COMMENT '连续对账未发现次数',
  ADD COLUMN `updated_at` datetime NULL COMMENT '索引更新时间';

ALTER TABLE `gb_recording_file`
  MODIFY COLUMN `start_time` datetime NULL,
  MODIFY COLUMN `time_len` decimal(12,3) NULL,
  MODIFY COLUMN `file_size` bigint unsigned NULL;

UPDATE `gb_recording_file` f
LEFT JOIN `gb_channel` c ON c.`id`=f.`channel_id`
LEFT JOIN `gb_device` d ON d.`device_id`=f.`device_id`
SET f.`channel_code`=COALESCE(c.`channel_id`,''),
    f.`channel_name`=COALESCE(NULLIF(c.`alias`,''),c.`name`,''),
    f.`device_name`=COALESCE(d.`name`,''),
    f.`owner_dept_id`=COALESCE(c.`owner_dept_id`,0),
    f.`record_date`=DATE(f.`start_time`),
    f.`discovered_at`=COALESCE(f.`created_at`,NOW()),
    f.`last_seen_at`=COALESCE(f.`created_at`,NOW()),
    f.`updated_at`=COALESCE(f.`created_at`,NOW()),
    f.`file_key`=LOWER(SHA2(CONCAT(CAST(f.`node_id` AS CHAR),CHAR(0),f.`file_path`),256));

ALTER TABLE `gb_recording_file`
  MODIFY COLUMN `file_key` varchar(64) NOT NULL,
  MODIFY COLUMN `discovered_at` datetime NOT NULL;

ALTER TABLE `gb_recording_file`
  DROP INDEX `uk_recording_file_node_path`,
  ADD UNIQUE KEY `uk_recording_file_key` (`file_key`),
  ADD KEY `idx_recording_file_date_tuple` (`node_id`,`vhost`,`app`,`stream`,`record_date`),
  ADD KEY `idx_recording_file_missing` (`missing_at`,`reconcile_miss_count`);

CREATE TABLE `gb_recording_reconcile_state` (
  `node_id` bigint NOT NULL,
  `status` varchar(16) NOT NULL DEFAULT 'queued',
  `trigger_source` varchar(16) NOT NULL DEFAULT 'scheduled',
  `requested_start` datetime NULL,
  `requested_end` datetime NULL,
  `effective_start` datetime NULL,
  `effective_end` datetime NULL,
  `started_at` datetime NULL,
  `finished_at` datetime NULL,
  `candidate_count` int NOT NULL DEFAULT '0',
  `success_count` int NOT NULL DEFAULT '0',
  `failure_count` int NOT NULL DEFAULT '0',
  `discovered_count` int NOT NULL DEFAULT '0',
  `inserted_count` int NOT NULL DEFAULT '0',
  `updated_count` int NOT NULL DEFAULT '0',
  `missing_count` int NOT NULL DEFAULT '0',
  `unattributed_count` int NOT NULL DEFAULT '0',
  `last_error` varchar(500) NOT NULL DEFAULT '',
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`node_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='云端录像节点最新对账状态';

SET @device_menu_id := (
  SELECT `id` FROM `sys_menu`
  WHERE `path` IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND `deleted_at` IS NULL
  ORDER BY CASE WHEN `path`='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,`id` LIMIT 1
);
SET @gb_parent_menu_id := (SELECT `parent_id` FROM `sys_menu` WHERE `id`=@device_menu_id);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`icon`,`sort`,`type`,`permission`,`hide`,`disable`,`created_at`,`updated_at`,`created_by`)
SELECT @gb_parent_menu_id,'/gb28181/cloud-recordings','gb28181-cloud-recordings','gb28181/cloud-recordings/index','云端录像','lucide:Cloud',35,2,'gb28181:recording:view',0,0,NOW(),NOW(),1
WHERE @gb_parent_menu_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL);
SET @recording_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/cloud-recordings' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`type`,`permission`,`hide`,`created_at`,`updated_at`,`created_by`)
SELECT @recording_menu_id,'','','','执行录像对账',3,'gb28181:recording:reconcile',1,NOW(),NOW(),1
WHERE @recording_menu_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:recording:reconcile' AND `deleted_at` IS NULL);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 云端录像',NOW(),NOW(),1 FROM (
  SELECT '查询云端录像列表' title,'/api/gb28181/cloud-recordings/files' path,'GET' method
  UNION ALL SELECT '查询云端录像选项','/api/gb28181/cloud-recordings/files/options','GET'
  UNION ALL SELECT '查询云端录像详情','/api/gb28181/cloud-recordings/files/:id','GET'
  UNION ALL SELECT '申请云端录像访问','/api/gb28181/cloud-recordings/files/:id/access','POST'
  UNION ALL SELECT '查询正在录像会话','/api/gb28181/cloud-recordings/active','GET'
  UNION ALL SELECT '查询录像对账状态','/api/gb28181/cloud-recordings/reconciliations','GET'
  UNION ALL SELECT '触发录像对账','/api/gb28181/cloud-recordings/reconciliations','POST'
) v WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT source_role.`role_id`,@recording_menu_id FROM `sys_role_menu` source_role
WHERE source_role.`menu_id`=@device_menu_id AND @recording_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=source_role.`role_id` AND x.`menu_id`=@recording_menu_id);
SET @reconcile_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `permission`='gb28181:recording:reconcile' AND `deleted_at` IS NULL);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`) SELECT 1,@reconcile_menu_id
WHERE @reconcile_menu_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` WHERE `role_id`=1 AND `menu_id`=@reconcile_menu_id);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @recording_menu_id,a.`id` FROM `sys_api` a
WHERE @recording_menu_id IS NOT NULL AND a.`path` LIKE '/api/gb28181/cloud-recordings/%'
  AND a.`path`<>'/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@recording_menu_id AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @reconcile_menu_id,a.`id` FROM `sys_api` a
WHERE @reconcile_menu_id IS NOT NULL AND a.`path`='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@reconcile_menu_id AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','','' FROM `sys_role_menu` rm
JOIN `sys_api` a ON a.`path` LIKE '/api/gb28181/cloud-recordings/%' AND a.`path`<>'/api/gb28181/cloud-recordings/reconciliations'
WHERE rm.`menu_id`=@recording_menu_id
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path`='/api/gb28181/cloud-recordings/reconciliations'
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
