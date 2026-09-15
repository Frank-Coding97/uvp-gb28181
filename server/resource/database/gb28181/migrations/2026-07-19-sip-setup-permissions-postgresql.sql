-- SIP setup API and administrator permissions (PostgreSQL, idempotent).

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 ('读取 SIP 配置状态','/api/gb28181/sip/setup/status','GET'),
 ('读取本机网络接口','/api/gb28181/sip/setup/network-interfaces','GET'),
 ('读取 SIP 平台信息','/api/gb28181/sip/platform','GET'),
 ('保存 SIP 配置','/api/gb28181/sip/setup/config','PUT'),
 ('暂缓 SIP 配置','/api/gb28181/sip/setup/skip','POST')
) AS v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT 140355,'','','',v.title,3,v.permission,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 ('查看 SIP 配置','gb28181:sip:config:view'),
 ('修改 SIP 配置','gb28181:sip:config:update')
) AS v(title,permission)
WHERE NOT EXISTS (SELECT 1 FROM sys_menu m WHERE m.permission=v.permission AND m.deleted_at IS NULL);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission IN ('gb28181:sip:config:view','gb28181:sip:config:update')
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu rm WHERE rm.role_id=1 AND rm.menu_id=m.id);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:sip:config:view' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.method IN ('PUT','POST')))
  AND a.path LIKE '/api/gb28181/sip/%'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a
WHERE a.path IN ('/api/gb28181/sip/setup/status','/api/gb28181/sip/setup/network-interfaces','/api/gb28181/sip/platform','/api/gb28181/sip/setup/config','/api/gb28181/sip/setup/skip')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
