-- Device/channel traffic accounting and runtime-monitor permissions (MySQL 5.7+).
CREATE TABLE IF NOT EXISTS `gb_device_traffic_session` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `business_key` VARCHAR(255) NOT NULL,
  `node_id` BIGINT NOT NULL,
  `media_server_uuid` VARCHAR(128) NOT NULL DEFAULT '',
  `zlm_session_id` VARCHAR(128) NOT NULL DEFAULT '',
  `direction` VARCHAR(16) NOT NULL,
  `device_code` VARCHAR(64) NOT NULL,
  `channel_code` VARCHAR(64) NOT NULL DEFAULT '',
  `owner_dept_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `media_kind` VARCHAR(32) NOT NULL DEFAULT '',
  `schema` VARCHAR(32) NOT NULL DEFAULT '',
  `vhost` VARCHAR(128) NOT NULL DEFAULT '',
  `app` VARCHAR(64) NOT NULL DEFAULT '',
  `stream` VARCHAR(255) NOT NULL DEFAULT '',
  `create_stamp` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `last_total_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `settled_total_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `duration_seconds` BIGINT NOT NULL DEFAULT 0,
  `state` VARCHAR(16) NOT NULL,
  `started_at` DATETIME(6) NULL,
  `last_seen_at` DATETIME(6) NULL,
  `ended_at` DATETIME(6) NULL,
  `unattributed_reason` VARCHAR(255) NOT NULL DEFAULT '',
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_traffic_session_business` (`business_key`),
  KEY `idx_traffic_session_node_state` (`node_id`,`state`),
  KEY `idx_traffic_session_zlm` (`zlm_session_id`),
  KEY `idx_traffic_session_direction` (`direction`),
  KEY `idx_traffic_session_device_started` (`device_code`,`channel_code`,`started_at`),
  KEY `idx_traffic_session_channel_started` (`channel_code`,`started_at`),
  KEY `idx_traffic_session_owner_dept` (`owner_dept_id`)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `gb_device_traffic_daily` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `stat_date` DATE NOT NULL,
  `device_code` VARCHAR(64) NOT NULL,
  `channel_code` VARCHAR(64) NOT NULL DEFAULT '',
  `owner_dept_id` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `upstream_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `downstream_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0,
  `upstream_duration_seconds` BIGINT NOT NULL DEFAULT 0,
  `downstream_duration_seconds` BIGINT NOT NULL DEFAULT 0,
  `upstream_sessions` BIGINT NOT NULL DEFAULT 0,
  `downstream_sessions` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_traffic_daily_scope` (`stat_date`,`device_code`,`channel_code`),
  KEY `idx_traffic_daily_device` (`device_code`),
  KEY `idx_traffic_daily_owner_dept` (`owner_dept_id`)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `gb_device_traffic_gap` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `node_id` BIGINT NOT NULL,
  `reason` VARCHAR(32) NOT NULL,
  `state` VARCHAR(16) NOT NULL,
  `started_at` DATETIME(6) NOT NULL,
  `ended_at` DATETIME(6) NULL,
  `detail` VARCHAR(500) NOT NULL DEFAULT '',
  `created_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  `updated_at` DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`id`),
  KEY `idx_traffic_gap_node_state` (`node_id`,`reason`,`state`),
  KEY `idx_traffic_gap_started` (`started_at`)
) ENGINE=InnoDB;

SET @traffic_device_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `path` IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`type`,`permission`,`hide`,`created_at`,`updated_at`,`created_by`)
SELECT @traffic_device_menu_id,'','','','查看运行监控',3,'gb28181:traffic:view',1,NOW(),NOW(),1
WHERE @traffic_device_menu_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:traffic:view' AND `deleted_at` IS NULL);
SET @traffic_menu_id := (SELECT MIN(`id`) FROM `sys_menu` WHERE `permission`='gb28181:traffic:view' AND `deleted_at` IS NULL);

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 运行监控',NOW(),NOW(),1 FROM (
  SELECT '查询流量汇总' title,'/api/gb28181/device-traffic/summary' path,'GET' method
  UNION ALL SELECT '查询流量趋势','/api/gb28181/device-traffic/trend','GET'
  UNION ALL SELECT '查询实时流量','/api/gb28181/device-traffic/realtime','GET'
  UNION ALL SELECT '查询流量会话','/api/gb28181/device-traffic/sessions','GET'
  UNION ALL SELECT '查询统计覆盖率','/api/gb28181/device-traffic/coverage','GET'
  UNION ALL SELECT '查询当前观看','/api/gb28181/device-traffic/viewers','GET'
  UNION ALL SELECT '强退观看连接','/api/gb28181/device-traffic/viewers/kick','POST'
) v WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT rm.`role_id`,@traffic_menu_id FROM `sys_role_menu` rm
WHERE rm.`menu_id`=@traffic_device_menu_id AND @traffic_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=rm.`role_id` AND x.`menu_id`=@traffic_menu_id);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,@traffic_menu_id WHERE @traffic_menu_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=@traffic_menu_id);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT @traffic_menu_id,a.`id` FROM `sys_api` a
WHERE a.`path` LIKE '/api/gb28181/device-traffic/%' AND a.`method`='GET' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=@traffic_menu_id AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm CROSS JOIN `sys_api` a
WHERE rm.`menu_id`=@traffic_menu_id AND a.`path` LIKE '/api/gb28181/device-traffic/%' AND a.`method`='GET' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path`='/api/gb28181/device-traffic/viewers/kick' AND a.`method`='POST' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
