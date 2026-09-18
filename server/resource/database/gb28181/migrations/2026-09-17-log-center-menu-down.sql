-- 回滚「日志中心」：菜单归位、标题还原、目录行移除（MySQL，幂等）
--
-- 恢复的是 up 执行前的实测值：sip-traces 是一级菜单 sort=90；
--   realtime-log / login-log / log 挂 /system（sort 2 / 1 / 0）；joblog 挂 /sysjobs（sort 0）。
-- 全部按 path 定位（menu_id 跨环境会漂移）；父目录用子查询取 id，并在取不到时保护不写 NULL。

UPDATE `sys_menu` SET `parent_id`=0, `sort`=90, `updated_at`=NOW()
WHERE `path`='/gb28181/sip-traces' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>90);

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t), `sort`=2, `updated_at`=NOW()
WHERE `path`='/system/realtime-log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>2)
  AND (SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t) IS NOT NULL;

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t), `sort`=1, `updated_at`=NOW()
WHERE `path`='/system/login-log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>1)
  AND (SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t) IS NOT NULL;

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t), `sort`=0, `updated_at`=NOW()
WHERE `path`='/system/log' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>0)
  AND (SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/system' AND `type`=1 AND `deleted_at` IS NULL) AS t) IS NOT NULL;

UPDATE `sys_menu` SET `parent_id`=(SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/sysjobs' AND `type`=1 AND `deleted_at` IS NULL) AS t), `sort`=0, `updated_at`=NOW()
WHERE `path`='/system/joblog' AND `deleted_at` IS NULL AND `type` IN (1,2) AND (`sort` IS NULL OR `sort`<>0)
  AND (SELECT `id` FROM (SELECT MIN(`id`) AS `id` FROM `sys_menu` WHERE `path`='/sysjobs' AND `type`=1 AND `deleted_at` IS NULL) AS t) IS NOT NULL;

UPDATE `sys_menu` SET `title`='log', `updated_at`=NOW()
WHERE `path`='/system/log' AND `deleted_at` IS NULL AND `title`='操作日志';

UPDATE `sys_menu` SET `title`='joblog', `updated_at`=NOW()
WHERE `path`='/system/joblog' AND `deleted_at` IS NULL AND `title`='定时任务日志';

DELETE FROM `sys_role_menu`
WHERE `menu_id` IN (SELECT `id` FROM (SELECT `id` FROM `sys_menu` WHERE `path`='/log-center') AS t);

DELETE FROM `sys_menu` WHERE `path`='/log-center';
