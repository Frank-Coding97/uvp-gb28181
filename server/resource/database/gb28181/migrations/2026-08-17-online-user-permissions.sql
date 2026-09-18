-- 在线用户菜单、API 与权限(MySQL 5.7+,幂等)。心跳只校验登录态,不进入业务权限。
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '查询在线用户','/api/sysOnlineUser/list','GET','系统管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysOnlineUser/list' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '强制下线会话','/api/sysOnlineUser/forceLogout','POST','系统管理',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/sysOnlineUser/forceLogout' AND `method`='POST' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`icon`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'/system/online-user','SystemOnlineUser','system/online-user/index','在线用户',0,0,8,2,'system:online-user:list','lucide:UsersRound',NOW(),NOW(),1
FROM `sys_menu` p
WHERE p.`path`='/system' AND p.`type`=1 AND p.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `path`='/system/online-user' AND `deleted_at` IS NULL)
ORDER BY p.`id` LIMIT 1;
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT p.`id`,'','SystemOnlineUserForceLogout','','强制下线',1,0,1,3,'system:online-user:force-logout',NOW(),NOW(),1
FROM `sys_menu` p
WHERE p.`path`='/system/online-user' AND p.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='system:online-user:force-logout' AND `deleted_at` IS NULL)
ORDER BY p.`id` LIMIT 1;

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m
WHERE (m.`path`='/system/online-user' OR m.`permission`='system:online-user:force-logout') AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=m.`id`);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m
JOIN `sys_api` a ON a.`path`='/api/sysOnlineUser/list' AND a.`method`='GET' AND a.`deleted_at` IS NULL
WHERE m.`path`='/system/online-user' AND m.`permission`='system:online-user:list' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m
JOIN `sys_api` a ON a.`path`='/api/sysOnlineUser/forceLogout' AND a.`method`='POST' AND a.`deleted_at` IS NULL
WHERE m.`permission`='system:online-user:force-logout' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id` AND m.`deleted_at` IS NULL
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id`
JOIN `sys_api` a ON a.`id`=ma.`api_id` AND a.`deleted_at` IS NULL
WHERE m.`permission` IN ('system:online-user:list','system:online-user:force-logout')
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
