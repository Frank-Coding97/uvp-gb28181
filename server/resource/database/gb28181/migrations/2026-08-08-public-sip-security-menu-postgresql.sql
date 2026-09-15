-- 国标接入安全菜单、API 和管理员默认权限 (PostgreSQL)
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 接入安全',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 ('查看安全快照','/api/gb28181/security/snapshot','GET'),('查看安全事件','/api/gb28181/security/events','GET'),('查看安全封禁','/api/gb28181/security/bans','GET'),('手工解封安全封禁','/api/gb28181/security/bans/:id/unban','POST'),('读取安全策略','/api/gb28181/security/policy','GET'),('修改安全策略','/api/gb28181/security/policy','PUT'),('查看防火墙 Agent 健康','/api/gb28181/security/agent/health','GET'),('订阅安全事件','/api/gb28181/security/stream','GET')) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
DO $$ DECLARE menu_id BIGINT; BEGIN
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,icon,created_at,updated_at,created_by) SELECT 0,'/gb28181/security','gb28181-security','gb28181/security/index','国标接入安全',0,0,5,2,'lucide:ShieldCheck',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/security' AND deleted_at IS NULL);
SELECT id INTO menu_id FROM sys_menu WHERE path='/gb28181/security' AND deleted_at IS NULL ORDER BY id LIMIT 1;
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,menu_id WHERE NOT EXISTS (SELECT 1 FROM sys_role_menu WHERE role_id=1 AND menu_id=menu_id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT menu_id,a.id FROM sys_api a WHERE a.path LIKE '/api/gb28181/security/%' AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=menu_id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path LIKE '/api/gb28181/security/%' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*'); END $$;
