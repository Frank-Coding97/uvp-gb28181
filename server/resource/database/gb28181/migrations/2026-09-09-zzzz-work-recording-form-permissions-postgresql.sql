-- Work recording form permissions (PostgreSQL, idempotent).
-- Start and stop may read the form; only the form permission may save it.
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL),0),'','Permission_gb28181_work_recording_form','','编辑作业表单',1,0,100,3,'gb28181:work-recording:form','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-recording:form' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission='gb28181:work-recording:form' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 作业录像',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT '我的作业列表' AS title,'/api/gb28181/work-recordings' AS path,'GET' AS method
 UNION ALL SELECT '查询作业表单','/api/gb28181/work-recordings/:id/form','GET'
 UNION ALL SELECT '保存作业草稿','/api/gb28181/work-recordings/:id/form','PUT'
) s
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.deleted_at IS NULL
  AND a.method='GET' AND a.path IN ('/api/gb28181/work-recordings','/api/gb28181/work-recordings/:id/form')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-recording:form' AND m.deleted_at IS NULL
  AND ((a.method='GET' AND a.path IN ('/api/gb28181/work-recordings','/api/gb28181/work-recordings/:id/form'))
    OR (a.method='PUT' AND a.path='/api/gb28181/work-recordings/:id/form'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
