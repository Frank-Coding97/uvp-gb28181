-- Channel video-share management permission and protected API registration.
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,created_at,updated_at,created_by)
SELECT m.id,'','','','Manage channel video sharing',3,'gb28181:video-share:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu m
WHERE m.path='/gb28181/device-mgmt/index'
  AND NOT EXISTS (SELECT 1 FROM sys_menu existing WHERE existing.permission='gb28181:video-share:manage');

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'Device management',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (
  SELECT 'Get channel video-share state' title,'/api/gb28181/channel-shares/:id' path,'GET' method
  UNION ALL SELECT 'Set channel video-share state','/api/gb28181/channel-shares/:id','PUT'
  UNION ALL SELECT 'Acquire channel video-share link','/api/gb28181/channel-shares/:id/link','POST'
  UNION ALL SELECT 'Regenerate channel video-share link','/api/gb28181/channel-shares/:id/regenerate','POST'
) v
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:video-share:manage'
  AND a.path IN ('/api/gb28181/channel-shares/:id','/api/gb28181/channel-shares/:id/link','/api/gb28181/channel-shares/:id/regenerate')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id
FROM sys_menu m
WHERE m.permission='gb28181:video-share:manage'
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu rm WHERE rm.role_id=1 AND rm.menu_id=m.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','',''
FROM sys_api a
WHERE a.path IN ('/api/gb28181/channel-shares/:id','/api/gb28181/channel-shares/:id/link','/api/gb28181/channel-shares/:id/regenerate')
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype='p' AND r.v0='role_1' AND r.v1=a.path AND r.v2=a.method);
