-- 登录审计事件(MySQL 5.7+,幂等)。不保存密码、验证码或 token。
CREATE TABLE IF NOT EXISTS `sys_login_logs` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `user_id` bigint unsigned DEFAULT NULL,
  `username` varchar(100) NOT NULL,
  `result` varchar(16) NOT NULL,
  `failure_reason` varchar(48) DEFAULT NULL,
  `ip` varchar(50) NOT NULL DEFAULT '',
  `location` varchar(100) NOT NULL DEFAULT '未知',
  `user_agent` varchar(500) NOT NULL DEFAULT '',
  `browser` varchar(100) NOT NULL DEFAULT '未知',
  `os` varchar(100) NOT NULL DEFAULT '未知',
  `created_at` datetime NOT NULL,
  `updated_at` datetime DEFAULT NULL,
  `deleted_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_login_logs_user_id` (`user_id`),
  KEY `idx_login_logs_username` (`username`),
  KEY `idx_login_logs_result` (`result`),
  KEY `idx_login_logs_failure_reason` (`failure_reason`),
  KEY `idx_login_logs_ip` (`ip`),
  KEY `idx_login_logs_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='后台登录审计日志';

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '登录日志列表','/api/sysLoginLog/list','GET','日志管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysLoginLog/list' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '登录日志详情','/api/sysLoginLog/:id','GET','日志管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysLoginLog/:id' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '删除登录日志','/api/sysLoginLog/delete','DELETE','日志管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysLoginLog/delete' AND `method`='DELETE' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '清空登录日志','/api/sysLoginLog/clear','POST','日志管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysLoginLog/clear' AND `method`='POST' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '解锁登录账号','/api/sysLoginLog/unlock','POST','日志管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysLoginLog/unlock' AND `method`='POST' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`icon`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/system/login-log','SystemLoginLog','system/login-log/index','登录日志',0,0,1,2,'system:login-log:list','lucide:FileClock',NOW(),NOW(),1
FROM `sys_menu` p WHERE p.`path`='/system' AND p.`type`=1 AND p.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/system/login-log' AND `deleted_at` IS NULL)
ORDER BY p.`id` LIMIT 1;
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','SystemLoginLogDelete','删除登录日志',1,0,1,3,'system:login-log:delete',NOW(),NOW(),1
FROM `sys_menu` m WHERE m.`permission`='system:login-log:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='system:login-log:delete' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','SystemLoginLogClear','清空登录日志',1,0,2,3,'system:login-log:clear',NOW(),NOW(),1
FROM `sys_menu` m WHERE m.`permission`='system:login-log:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='system:login-log:clear' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','SystemLoginLogUnlock','解锁登录账号',1,0,3,3,'system:login-log:unlock',NOW(),NOW(),1
FROM `sys_menu` m WHERE m.`permission`='system:login-log:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='system:login-log:unlock' AND `deleted_at` IS NULL);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m WHERE m.`path`='/system/login-log' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=m.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`path`='/api/sysLoginLog/list' AND a.`method`='GET' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`path`='/api/sysLoginLog/:id' AND a.`method`='GET' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m WHERE m.`permission` IN ('system:login-log:delete','system:login-log:clear','system:login-log:unlock') AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=m.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`path`='/api/sysLoginLog/delete' AND a.`method`='DELETE' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:delete' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`path`='/api/sysLoginLog/clear' AND a.`method`='POST' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:clear' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`path`='/api/sysLoginLog/unlock' AND a.`method`='POST' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:unlock' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` AND m.`deleted_at` IS NULL
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id` JOIN `sys_api` a ON a.`id`=ma.`api_id` AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:login-log:list'
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm JOIN `sys_menu` m ON m.`id`=rm.`menu_id` AND m.`deleted_at` IS NULL
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id` JOIN `sys_api` a ON a.`id`=ma.`api_id` AND a.`deleted_at` IS NULL
WHERE m.`permission` IN ('system:login-log:delete','system:login-log:clear','system:login-log:unlock')
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
