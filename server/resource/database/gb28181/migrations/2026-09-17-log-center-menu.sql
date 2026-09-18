-- 日志中心：把散落的日志菜单收进新一级目录 /log-center（MySQL，幂等）
--
-- 背景：后端 GetRouters（server/app/controllers/sysmenu.go）取「角色授权的 menu_id 扁平集合」
--   再交给 BuildTree 组树；父目录不在授权集合里 → 子树整体变孤儿 → 只打一条
--   menu tree orphan detected 警告就从侧栏静默消失。因此新增目录必须同步补角色授权。
-- 一律按 path 定位：menu_id 由各环境自增分配，跨环境会漂移。
--
-- 收录 5 项：SIP 日志 / 实时日志控制台 / 登录日志 / 操作日志 / 定时任务日志
-- 目录 sort=145，排在流媒体管理(140)之后、系统管理(150)之前；原 SIP 日志占用的 90 空出。
-- 不搬 /media/scheduling（调度日志）：它是流媒体工作台的 tab，前端多处逻辑按 path 绑死。

INSERT INTO `sys_menu` (`parent_id`,`title`,`path`,`name`,`sort`,`icon`,`type`,`hide`,`disable`,`keep_alive`,`created_at`,`updated_at`)
SELECT 0,'日志中心','/log-center','LogCenter',145,'lucide:FileClock',1,0,0,1,NOW(),NOW()
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM (SELECT `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t), `sort`=10, `updated_at`=NOW()
WHERE `path`='/gb28181/sip-traces' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>10);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t), `sort`=20, `updated_at`=NOW()
WHERE `path`='/system/realtime-log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>20);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t), `sort`=30, `updated_at`=NOW()
WHERE `path`='/system/login-log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>30);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t), `sort`=40, `updated_at`=NOW()
WHERE `path`='/system/log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>40);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/log-center' AND `deleted_at` IS NULL) AS t), `sort`=50, `updated_at`=NOW()
WHERE `path`='/system/joblog' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>50);

UPDATE `sys_menu` SET `title`='操作日志', `updated_at`=NOW()
WHERE `path`='/system/log' AND `deleted_at` IS NULL AND `title`<>'操作日志';

UPDATE `sys_menu` SET `title`='定时任务日志', `updated_at`=NOW()
WHERE `path`='/system/joblog' AND `deleted_at` IS NULL AND `title`<>'定时任务日志';

INSERT IGNORE INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT DISTINCT rm.`role_id`, lc.`id`
FROM `sys_role_menu` rm
JOIN `sys_menu` m  ON m.`id`=rm.`menu_id`
JOIN `sys_menu` lc ON lc.`path`='/log-center' AND lc.`deleted_at` IS NULL
WHERE m.`deleted_at` IS NULL
  AND m.`path` IN ('/gb28181/sip-traces','/system/realtime-log','/system/login-log','/system/log','/system/joblog');
