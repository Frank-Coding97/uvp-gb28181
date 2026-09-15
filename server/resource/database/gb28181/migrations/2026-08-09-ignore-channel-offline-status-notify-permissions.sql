-- 忽略通道离线/异常通知配置 API 与既有 SIP 配置权限关联（MySQL 5.7+，幂等）。

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '读取忽略通道离线异常通知配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','GET','GB28181 SIP 配置',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT '修改忽略通道离线异常通知配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','PUT','GB28181 SIP 配置',NOW(),NOW(),1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND `method`='PUT' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE ((m.`permission`='gb28181:sip:config:view' AND a.`method`='GET')
    OR (m.`permission`='gb28181:sip:config:update' AND a.`method`='PUT'))
  AND m.`deleted_at` IS NULL
  AND a.`path`='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
JOIN `sys_api` a ON a.`path`='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify'
WHERE ((m.`permission`='gb28181:sip:config:view' AND a.`method`='GET')
    OR (m.`permission`='gb28181:sip:config:update' AND a.`method`='PUT'))
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path`='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.`method` IN ('GET','PUT') AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
