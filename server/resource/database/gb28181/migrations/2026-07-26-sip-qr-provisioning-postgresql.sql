-- 扫码接入二维码: token 生成端点权限 (PostgreSQL, idempotent).
--
-- 权限点复用既有 gb28181:sip:config:view,不新增 sys_menu 权限行。
-- 兑换端点 /api/gb28181/sip/qr/exchange 与引导页 /gb28181/qr 免鉴权,刻意不入表。

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT '生成设备接入二维码','/api/gb28181/sip/qr/token','POST','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:sip:config:view' AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a
WHERE a.path='/api/gb28181/sip/qr/token' AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
