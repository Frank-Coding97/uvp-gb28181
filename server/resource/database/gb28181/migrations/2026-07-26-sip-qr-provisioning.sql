-- 扫码接入二维码: token 生成端点权限 (MySQL 5.7+, idempotent).
--
-- 权限点复用既有 gb28181:sip:config:view —— 生成接入码等于下发平台统一密码,
-- 门槛应与"查看明文密码"齐平,故不新增 sys_menu 权限行(避免既有角色升级后需手动授权).
--
-- 注意: 兑换端点 /api/gb28181/sip/qr/exchange 与引导页 /gb28181/qr 都是免鉴权,
-- 走 public 组不经 Casbin,故**刻意不入表**。

INSERT INTO `sys_api` (`title`, `path`, `method`, `api_group`, `created_at`, `updated_at`, `created_by`)
SELECT '生成设备接入二维码', '/api/gb28181/sip/qr/token', 'POST', 'GB28181 SIP 配置', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` WHERE `path`='/api/gb28181/sip/qr/token' AND `method`='POST' AND `deleted_at` IS NULL);

-- 关联到既有"查看 SIP 配置"权限点。
-- 这里按 path 显式关联而非按 method 匹配 —— 样板中 view 权限点只关联 GET,
-- 而本端点是 POST,套用方法匹配会漏掉。
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`)
SELECT m.id, a.id FROM `sys_menu` m JOIN `sys_api` a
WHERE m.permission='gb28181:sip:config:view'
  AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM `sys_menu_api` ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO `sys_casbin_rule` (`ptype`,`v0`,`v1`,`v2`,`v3`,`v4`,`v5`)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM `sys_api` a
WHERE a.path='/api/gb28181/sip/qr/token' AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM `sys_casbin_rule` c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
