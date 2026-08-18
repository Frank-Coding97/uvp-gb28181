CREATE TABLE IF NOT EXISTS sys_login_logs (
  id BIGSERIAL PRIMARY KEY, user_id BIGINT NULL, username VARCHAR(100) NOT NULL,
  result VARCHAR(16) NOT NULL, failure_reason VARCHAR(48), ip VARCHAR(50) NOT NULL DEFAULT '',
  location VARCHAR(100) NOT NULL DEFAULT '未知', user_agent VARCHAR(500) NOT NULL DEFAULT '',
  browser VARCHAR(100) NOT NULL DEFAULT '未知', os VARCHAR(100) NOT NULL DEFAULT '未知',
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NULL, deleted_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS idx_login_logs_user_id ON sys_login_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_login_logs_username ON sys_login_logs(username);
CREATE INDEX IF NOT EXISTS idx_login_logs_result ON sys_login_logs(result);
CREATE INDEX IF NOT EXISTS idx_login_logs_failure_reason ON sys_login_logs(failure_reason);
CREATE INDEX IF NOT EXISTS idx_login_logs_ip ON sys_login_logs(ip);
CREATE INDEX IF NOT EXISTS idx_login_logs_created_at ON sys_login_logs(created_at);
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES ('登录日志列表','/api/sysLoginLog/list','GET'),('登录日志详情','/api/sysLoginLog/:id','GET')) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT p.id,'/system/login-log','SystemLoginLog','system/login-log/index','登录日志',0,0,1,2,'system:login-log:list','lucide:FileClock',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM sys_menu p WHERE p.path='/system' AND p.type=1 AND p.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/system/login-log' AND deleted_at IS NULL) ORDER BY p.id LIMIT 1;
INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.path='/system/login-log' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.path IN ('/api/sysLoginLog/list','/api/sysLoginLog/:id') AND a.method='GET' AND a.deleted_at IS NULL WHERE m.permission='system:login-log:list' AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_'||rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='system:login-log:list' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
