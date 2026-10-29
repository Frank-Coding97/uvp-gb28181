-- HTTPS 管理复用“修改系统配置”权限；不扩大普通角色权限。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'HTTPS 管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查看 HTTPS 状态' title,'/api/system/https/status' path,'GET' method UNION ALL SELECT '检测 HTTPS 端口' title,'/api/system/https/check-port' path,'POST' method UNION ALL SELECT '校验证书' title,'/api/system/https/certificate' path,'POST' method UNION ALL SELECT '应用 HTTPS 配置' title,'/api/system/https/apply' path,'POST' method) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='system:config:update' AND m.deleted_at IS NULL AND a.path IN ('/api/system/https/status','/api/system/https/check-port','/api/system/https/certificate','/api/system/https/apply')
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p',roles.v0,a.path,a.method,'*','','' FROM
(SELECT v0 FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/config/update' AND v2='PUT'
 UNION SELECT 'role_1' FROM sys_role WHERE id=1 AND status=1 AND deleted_at IS NULL) roles
CROSS JOIN sys_api a WHERE a.path IN ('/api/system/https/status','/api/system/https/check-port','/api/system/https/certificate','/api/system/https/apply')
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype='p' AND r.v0=roles.v0 AND r.v1=a.path AND r.v2=a.method);
