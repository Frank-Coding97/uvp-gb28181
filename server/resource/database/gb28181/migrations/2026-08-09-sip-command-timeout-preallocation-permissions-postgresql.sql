-- SIP 命令超时与预分配模式配置 API 权限（PostgreSQL，幂等）。

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 ('读取 SIP 命令超时时间','/api/gb28181/sip/service-config/sip-command-timeout','GET'),
 ('修改 SIP 命令超时时间','/api/gb28181/sip/service-config/sip-command-timeout','PUT'),
 ('读取预分配模式','/api/gb28181/sip/service-config/preallocation-mode','GET'),
 ('修改预分配模式','/api/gb28181/sip/service-config/preallocation-mode','PUT')
) AS v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:sip:config:view' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.method='PUT'))
  AND m.deleted_at IS NULL
  AND a.path IN ('/api/gb28181/sip/service-config/sip-command-timeout','/api/gb28181/sip/service-config/preallocation-mode')
  AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_' || rm.role_id::text,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:sip:config:view' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.method='PUT'))
  AND m.deleted_at IS NULL
  AND a.path IN ('/api/gb28181/sip/service-config/sip-command-timeout','/api/gb28181/sip/service-config/preallocation-mode')
  AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id::text AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a
WHERE a.path IN ('/api/gb28181/sip/service-config/sip-command-timeout','/api/gb28181/sip/service-config/preallocation-mode')
  AND a.method IN ('GET','PUT') AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
