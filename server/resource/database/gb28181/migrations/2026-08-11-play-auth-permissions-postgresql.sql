-- 播放鉴权配置与独立点播权限（PostgreSQL，幂等）。
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 ('读取播放鉴权配置','/api/gb28181/sip/service-config/play-auth','GET'),
 ('修改播放鉴权配置','/api/gb28181/sip/service-config/play-auth','PUT'),
 ('发起实时点播','/api/gb28181/play/:deviceId/:channelId','POST'),
 ('申请固定播放地址授权','/api/gb28181/play/:deviceId/:channelId/authorization','POST')
) AS v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,title,type,permission,hide,created_at,updated_at,created_by)
SELECT 140355,'','','发起实时点播',3,'gb28181:play:start',TRUE,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:start' AND deleted_at IS NULL);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission='gb28181:play:start' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

-- 早期版本曾把预授权 POST 错挂到 SIP 配置查看权限；升级时精确撤销该派生授权。
DELETE FROM sys_casbin_rule c
USING sys_role_menu rm, sys_menu m
WHERE c.v0='role_' || rm.role_id::text AND m.id=rm.menu_id
  AND c.ptype='p' AND m.permission IN ('gb28181:sip:config:view','gb28181:sip:config:update')
  AND c.v1='/api/gb28181/play/:deviceId/:channelId/authorization' AND c.v2='POST';
DELETE FROM sys_menu_api ma
USING sys_menu m, sys_api a
WHERE m.id=ma.menu_id AND a.id=ma.api_id
  AND m.permission IN ('gb28181:sip:config:view','gb28181:sip:config:update')
  AND a.path='/api/gb28181/play/:deviceId/:channelId/authorization' AND a.method='POST';

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:sip:config:view' AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='PUT'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:play:start'
  AND ((a.path='/api/gb28181/play/:deviceId/:channelId' AND a.method='POST')
    OR (a.path='/api/gb28181/play/:deviceId/:channelId/authorization' AND a.method='POST'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_' || rm.role_id::text,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id CROSS JOIN sys_api a
WHERE ((m.permission='gb28181:sip:config:view' AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='GET')
    OR (m.permission='gb28181:sip:config:update' AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='PUT'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id::text AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_' || rm.role_id::text,a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id CROSS JOIN sys_api a
WHERE m.permission='gb28181:play:start'
  AND ((a.path='/api/gb28181/play/:deviceId/:channelId' AND a.method='POST')
    OR (a.path='/api/gb28181/play/:deviceId/:channelId/authorization' AND a.method='POST'))
  AND m.deleted_at IS NULL
  AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id::text AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
