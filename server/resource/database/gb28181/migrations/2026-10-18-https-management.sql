-- HTTPS 管理复用“修改系统配置”权限；不扩大普通角色权限。
-- ⚠️ MySQL 各表排序规则历史上并不统一（sys_api 为 utf8mb4_unicode_ci、
-- sys_casbin_rule 为 utf8mb4_0900_ai_ci、连接默认又是 utf8mb4_0900_ai_ci），
-- 跨表/跨派生表的字符串列比较会报 1267 Illegal mix of collations。
-- 因此与 2026-10-16-service-upgrade-permission.sql 一致：参与比较的文本两侧
-- 一律显式 BINARY，按字节精确比较，不受库内 collation 差异影响。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'HTTPS 管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (SELECT '查看 HTTPS 状态' title,'/api/system/https/status' path,'GET' method UNION ALL SELECT '检测 HTTPS 端口' title,'/api/system/https/check-port' path,'POST' method UNION ALL SELECT '校验证书' title,'/api/system/https/certificate' path,'POST' method UNION ALL SELECT '应用 HTTPS 配置' title,'/api/system/https/apply' path,'POST' method) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE BINARY a.path=BINARY v.path AND BINARY a.method=BINARY v.method);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE BINARY m.permission=BINARY 'system:config:update' AND m.deleted_at IS NULL AND BINARY a.path IN (BINARY '/api/system/https/status',BINARY '/api/system/https/check-port',BINARY '/api/system/https/certificate',BINARY '/api/system/https/apply')
AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p',roles.v0,a.path,a.method,'*','','' FROM
(SELECT v0 FROM sys_casbin_rule WHERE BINARY ptype=BINARY 'p' AND BINARY v1=BINARY '/api/config/update' AND BINARY v2=BINARY 'PUT'
 UNION SELECT 'role_1' FROM sys_role WHERE id=1 AND status=1 AND deleted_at IS NULL) roles
CROSS JOIN sys_api a WHERE BINARY a.path IN (BINARY '/api/system/https/status',BINARY '/api/system/https/check-port',BINARY '/api/system/https/certificate',BINARY '/api/system/https/apply')
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE BINARY r.ptype=BINARY 'p' AND BINARY r.v0=BINARY roles.v0 AND BINARY r.v1=BINARY a.path AND BINARY r.v2=BINARY a.method);
