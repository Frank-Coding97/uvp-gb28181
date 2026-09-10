-- 作业单表单历史值接口权限（MySQL 5.7+，幂等）。
-- 「录入后寄存，下拉选用」对应的 GET /api/gb28181/work-orders/form-history
-- 挂在 view 权限（gb28181:work-order:view）下：弹窗打开就拉历史，
-- 与列表查询是同一个查看语义，不需要单独的权限点。

-- 1) API 记录
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '作业单表单历史值','/api/gb28181/work-orders/form-history','GET','GB28181 作业单',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`='/api/gb28181/work-orders/form-history' AND a.`method`='GET' AND a.`deleted_at` IS NULL);

-- 2) view 权限的菜单-API 精确绑定
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a ON a.`deleted_at` IS NULL
WHERE m.`permission`='gb28181:work-order:view' AND m.`deleted_at` IS NULL
  AND a.`method`='GET' AND a.`path`='/api/gb28181/work-orders/form-history'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

-- 3) Casbin 规则：role_1 命中 view → 自动覆盖 form-history（与既有 view 链路一致）
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT DISTINCT 'p','role_1',a.`path`,a.`method`,'*','',''
FROM `sys_api` a
WHERE a.`path`='/api/gb28181/work-orders/form-history' AND a.`method`='GET' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
