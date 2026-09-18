-- SIP setup API and administrator permissions (MySQL 5.7+, idempotent).

INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '读取 SIP 配置状态', '/api/gb28181/sip/setup/status', 'GET', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/setup/status' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '读取本机网络接口', '/api/gb28181/sip/setup/network-interfaces', 'GET', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/setup/network-interfaces' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '读取 SIP 平台信息', '/api/gb28181/sip/platform', 'GET', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/platform' AND `method`='GET' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '保存 SIP 配置', '/api/gb28181/sip/setup/config', 'PUT', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/setup/config' AND `method`='PUT' AND `deleted_at` IS NULL);
INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '暂缓 SIP 配置', '/api/gb28181/sip/setup/skip', 'POST', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/setup/skip' AND `method`='POST' AND `deleted_at` IS NULL);

INSERT INTO `sys_menu` (`parent_id`, `path`, `name`, `component`, `title`, `type`, `permission`, `hide`, `created_at`, `updated_at`, `created_by`)
SELECT 140355, '', '', '', '查看 SIP 配置', 3, 'gb28181:sip:config:view', 1, NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:sip:config:view' AND `deleted_at` IS NULL);
INSERT INTO `sys_menu` (`parent_id`, `path`, `name`, `component`, `title`, `type`, `permission`, `hide`, `created_at`, `updated_at`, `created_by`)
SELECT 140355, '', '', '', '修改 SIP 配置', 3, 'gb28181:sip:config:update', 1, NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_menu` WHERE `permission`='gb28181:sip:config:update' AND `deleted_at` IS NULL);

INSERT INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT 1, m.id FROM `sys_menu` m
WHERE m.permission IN ('gb28181:sip:config:view','gb28181:sip:config:update')
  AND NOT EXISTS (SELECT 1 FROM `sys_role_menu` rm WHERE rm.role_id=1 AND rm.menu_id=m.id);

INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT m.id, a.id FROM `sys_menu` m JOIN `sys_api` a
WHERE ((m.permission='gb28181:sip:config:view' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.method IN ('PUT','POST')))
  AND a.path LIKE '/api/gb28181/sip/%'
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM `sys_api` a
WHERE a.path LIKE '/api/gb28181/sip/%'
  AND a.path IN ('/api/gb28181/sip/setup/status','/api/gb28181/sip/setup/network-interfaces','/api/gb28181/sip/platform','/api/gb28181/sip/setup/config','/api/gb28181/sip/setup/skip')
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
