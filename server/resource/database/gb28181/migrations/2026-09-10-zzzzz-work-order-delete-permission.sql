-- 作业单删除权限（MySQL 5.7+，幂等）。
-- 列表页新增「删除」与「批量删除」两个动作，对应两条接口：
--   DELETE /api/gb28181/work-orders/:id          单条删除
--   POST   /api/gb28181/work-orders/batch-delete 勾选批量删除
-- 删除只允许作用于已结束/失败的作业单，正在录制的由服务端跳过并回报，
-- 所以它是一项独立于「结束录像」的权限，单独授予。
--
-- 命名用 zzzzz-，保证排在 zz-work-order-menu（菜单本身）与
-- zzzz-work-recording-retire（旧权限退役）之后：先接管、再退场、最后补权限。

-- 1) 按钮权限（挂在作业单菜单下）
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`disable`,`sort`,`type`,`permission`,`icon`,`created_at`,`updated_at`,`created_by`)
SELECT COALESCE((SELECT MIN(`id`) FROM `sys_menu` WHERE `path`='/gb28181/work-orders' AND `type`=2 AND `deleted_at` IS NULL),0),'','Permission_gb28181_work_order_delete','','删除作业单',1,0,4,3,'gb28181:work-order:delete','',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:work-order:delete' AND `deleted_at` IS NULL);

-- 2) 角色绑定
INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m
WHERE m.`permission`='gb28181:work-order:delete' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` x WHERE x.`role_id`=1 AND x.`menu_id`=m.`id`);

-- 3) API 权限
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT s.title,s.path,s.method,'GB28181 作业单',NOW(),NOW(),1 FROM (
 SELECT '删除作业单' AS title,'/api/gb28181/work-orders/:id' AS path,'DELETE' AS method
 UNION ALL SELECT '批量删除作业单','/api/gb28181/work-orders/batch-delete','POST'
) s
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=s.path AND a.`method`=s.method AND a.`deleted_at` IS NULL);

-- 4) 菜单与 API 的精确绑定
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`deleted_at` IS NULL
WHERE m.`permission`='gb28181:work-order:delete' AND m.`deleted_at` IS NULL
  AND ((a.`method`='DELETE' AND a.`path`='/api/gb28181/work-orders/:id')
    OR (a.`method`='POST' AND a.`path`='/api/gb28181/work-orders/batch-delete'))
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

-- 5) Casbin 规则（同一角色可能通过菜单与按钮命中同一 API，写入前必须去重）
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT DISTINCT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id`
JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE m.`permission`='gb28181:work-order:delete'
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
