-- 全局订阅项目和新通道音频默认值 API 权限（MySQL 5.7+，幂等）。

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.`title`,v.`path`,v.`method`,'GB28181 SIP 配置',NOW(),NOW(),1
FROM (
  SELECT '读取全局订阅项目' AS `title`,'/api/gb28181/sip/service-config/global-subscriptions' AS `path`,'GET' AS `method`
  UNION ALL SELECT '修改全局订阅项目','/api/gb28181/sip/service-config/global-subscriptions','PUT'
  UNION ALL SELECT '读取全局通道音频配置','/api/gb28181/sip/service-config/default-channel-audio','GET'
  UNION ALL SELECT '修改全局通道音频配置','/api/gb28181/sip/service-config/default-channel-audio','PUT'
) v
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.`path`=v.`path` AND a.`method`=v.`method` AND a.`deleted_at` IS NULL);

INSERT INTO `sys_menu_api` (`menu_id`,`api_id`)
SELECT m.`id`,a.`id` FROM `sys_menu` m JOIN `sys_api` a
WHERE ((m.`permission`='gb28181:sip:config:view' AND a.`method`='GET')
    OR (m.`permission`='gb28181:sip:config:update' AND a.`method`='PUT'))
  AND m.`deleted_at` IS NULL
  AND a.`path` IN ('/api/gb28181/sip/service-config/global-subscriptions','/api/gb28181/sip/service-config/default-channel-audio')
  AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` x WHERE x.`menu_id`=m.`id` AND x.`api_id`=a.`id`);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p',CONCAT('role_',rm.`role_id`),a.`path`,a.`method`,'*','',''
FROM `sys_role_menu` rm
JOIN `sys_menu` m ON m.`id`=rm.`menu_id`
CROSS JOIN `sys_api` a
WHERE ((m.`permission`='gb28181:sip:config:view' AND a.`method`='GET')
    OR (m.`permission`='gb28181:sip:config:update' AND a.`method`='PUT'))
  AND m.`deleted_at` IS NULL AND a.`deleted_at` IS NULL
  AND a.`path` IN ('/api/gb28181/sip/service-config/global-subscriptions','/api/gb28181/sip/service-config/default-channel-audio')
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`=CONCAT('role_',rm.`role_id`) AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.`path`,a.`method`,'*','','' FROM `sys_api` a
WHERE a.`path` IN ('/api/gb28181/sip/service-config/global-subscriptions','/api/gb28181/sip/service-config/default-channel-audio')
  AND a.`method` IN ('GET','PUT') AND a.`deleted_at` IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.`ptype`='p' AND c.`v0`='role_1' AND c.`v1`=a.`path` AND c.`v2`=a.`method` AND c.`v3`='*');
