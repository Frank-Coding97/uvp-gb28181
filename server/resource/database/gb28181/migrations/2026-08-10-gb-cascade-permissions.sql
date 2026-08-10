-- GB28181 cascade API/menu/Casbin seed (MySQL 5.7+, idempotent).
-- The cascade runtime remains protected by the normal JWT/Casbin middleware.
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 国标级联',NOW(),NOW(),1
FROM (
  SELECT '查看级联平台列表' title,'/api/gb28181/cascade/platforms' path,'GET' method UNION ALL
  SELECT '创建级联平台','/api/gb28181/cascade/platforms','POST' UNION ALL
  SELECT '查看级联平台','/api/gb28181/cascade/platforms/:id','GET' UNION ALL
  SELECT '修改级联平台','/api/gb28181/cascade/platforms/:id','PUT' UNION ALL
  SELECT '删除级联平台','/api/gb28181/cascade/platforms/:id','DELETE' UNION ALL
  SELECT '启用级联平台','/api/gb28181/cascade/platforms/:id/enable','POST' UNION ALL
  SELECT '停用级联平台','/api/gb28181/cascade/platforms/:id/disable','POST' UNION ALL
  SELECT '更新级联启用状态','/api/gb28181/cascade/platforms/:id/enabled','PUT' UNION ALL
  SELECT '重连级联平台','/api/gb28181/cascade/platforms/:id/reconnect','POST' UNION ALL
  SELECT '查看级联共享','/api/gb28181/cascade/platforms/:id/shares','GET' UNION ALL
  SELECT '更新级联共享','/api/gb28181/cascade/platforms/:id/shares','PUT'
) v
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','gb28181-cascade-view','','查看国标级联',1,3,'gb28181:cascade:view',NOW(),NOW(),1
FROM `sys_menu` m
WHERE m.`path`='/gb28181' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`permission`='gb28181:cascade:view' AND x.`deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','gb28181-cascade-manage','','管理国标级联',1,3,'gb28181:cascade:manage',NOW(),NOW(),1
FROM `sys_menu` m
WHERE m.`path`='/gb28181' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`permission`='gb28181:cascade:manage' AND x.`deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','gb28181-cascade-enable','','启停国标级联',1,3,'gb28181:cascade:enable',NOW(),NOW(),1
FROM `sys_menu` m
WHERE m.`path`='/gb28181' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`permission`='gb28181:cascade:enable' AND x.`deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','gb28181-cascade-share','','共享国标级联资源',1,3,'gb28181:cascade:share',NOW(),NOW(),1
FROM `sys_menu` m
WHERE m.`path`='/gb28181' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`permission`='gb28181:cascade:share' AND x.`deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`,`path`,`name`,`component`,`title`,`hide`,`type`,`permission`,`created_at`,`updated_at`,`created_by`)
SELECT m.`id`,'','gb28181-cascade-reconnect','','重连国标级联',1,3,'gb28181:cascade:reconnect',NOW(),NOW(),1
FROM `sys_menu` m
WHERE m.`path`='/gb28181' AND m.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu` x WHERE x.`permission`='gb28181:cascade:reconnect' AND x.`deleted_at` IS NULL);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE m.`permission`='gb28181:cascade:view' AND a.`path` LIKE '/api/gb28181/cascade/%'
  AND a.`method`='GET' AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE m.`permission`='gb28181:cascade:manage' AND a.`path` IN ('/api/gb28181/cascade/platforms','/api/gb28181/cascade/platforms/:id')
  AND a.`method` IN ('POST','PUT','DELETE') AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE m.`permission`='gb28181:cascade:enable' AND a.`path` LIKE '/api/gb28181/cascade/platforms/%/enab%'
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE m.`permission`='gb28181:cascade:share' AND a.`path`='/api/gb28181/cascade/platforms/:id/shares'
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE m.`permission`='gb28181:cascade:reconnect' AND a.`path`='/api/gb28181/cascade/platforms/:id/reconnect'
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_role_menu` (`role_id`,`menu_id`)
SELECT 1,m.`id` FROM `sys_menu` m
WHERE m.`permission` IN ('gb28181:cascade:view','gb28181:cascade:manage','gb28181:cascade:enable','gb28181:cascade:share','gb28181:cascade:reconnect')
  AND m.`deleted_at` IS NULL AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` r WHERE r.`role_id`=1 AND r.`menu_id`=m.`id`);
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path` LIKE '/api/gb28181/cascade/%' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
