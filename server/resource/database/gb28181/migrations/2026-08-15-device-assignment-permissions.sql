-- 设备分配/共享授权 API 权限(sys_api + menu_api + casbin,MySQL 5.7+,幂等)。
-- 参照 2026-08-10-playback-settings-permissions.sql 的体系。

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title, v.path, v.method, 'GB28181 设备分配', NOW(), NOW(), 1
FROM (
    SELECT '调整设备归属' AS title, '/api/gb28181/device-mgmt/assign' AS path, 'POST' AS method
    UNION ALL SELECT '整部门调整归属', '/api/gb28181/device-mgmt/assign-dept', 'POST'
    UNION ALL SELECT '查询设备共享授权', '/api/gb28181/device-mgmt/device/:id/grants', 'GET'
    UNION ALL SELECT '添加设备共享授权', '/api/gb28181/device-mgmt/device/:id/grants', 'POST'
    UNION ALL SELECT '取消设备共享授权', '/api/gb28181/device-mgmt/device/:id/grants/:grantId', 'DELETE'
) AS v
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.path AND a.`method`=v.method AND a.`deleted_at` IS NULL);

-- 按钮权限 → API 绑定(assign 按钮绑分配类,share 按钮绑共享类)
INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`, a.`id` FROM `sys_menu` m CROSS JOIN `sys_api` a
WHERE m.`permission`='gb28181:device:assign' AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND a.`path` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept')
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`, a.`id` FROM `sys_menu` m CROSS JOIN `sys_api` a
WHERE m.`permission`='gb28181:device:share' AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND a.`path` IN ('/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId')
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

-- Casbin:已绑菜单的角色同步规则 + role_1 兜底
INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT DISTINCT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
JOIN `sys_menu_api` ma ON ma.`menu_id`=m.`id`
JOIN `sys_api` a ON a.`id`=ma.`api_id`
WHERE m.`permission` IN ('gb28181:device:assign','gb28181:device:share')
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND a.`path` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept',
                   '/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId')
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path` IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept',
                   '/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId')
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method`);
