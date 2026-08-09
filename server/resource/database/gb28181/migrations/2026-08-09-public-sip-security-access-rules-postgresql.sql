-- Manual SIP blacklist/allowlist rules and protected APIs (PostgreSQL)
CREATE TABLE IF NOT EXISTS gb_sip_security_access_rule (
  id BIGSERIAL PRIMARY KEY, list_type VARCHAR(16) NOT NULL, match_type VARCHAR(16) NOT NULL,
  match_value VARCHAR(255) NOT NULL, scope VARCHAR(32) NOT NULL DEFAULT 'all_sip',
  status VARCHAR(16) NOT NULL DEFAULT 'enabled', expires_at TIMESTAMP NULL,
  note VARCHAR(255) NOT NULL DEFAULT '', created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL,
  CONSTRAINT uk_gb_sip_security_access_rule_match UNIQUE (list_type,match_type,match_value)
);
CREATE INDEX IF NOT EXISTS idx_gb_sip_security_access_rule_list_status ON gb_sip_security_access_rule(list_type,status);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 接入安全',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 ('查看安全访问名单','/api/gb28181/security/access-rules','GET'),
 ('创建安全访问规则','/api/gb28181/security/access-rules','POST'),
 ('修改安全访问规则','/api/gb28181/security/access-rules/:id','PUT'),
 ('删除安全访问规则','/api/gb28181/security/access-rules/:id','DELETE')) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
DO $$ DECLARE security_menu_id BIGINT; BEGIN
SELECT id INTO security_menu_id FROM sys_menu WHERE path='/gb28181/security' AND deleted_at IS NULL ORDER BY id LIMIT 1;
INSERT INTO sys_menu_api(menu_id,api_id) SELECT security_menu_id,a.id FROM sys_api a WHERE security_menu_id IS NOT NULL AND a.path LIKE '/api/gb28181/security/access-rules%' AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=security_menu_id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path LIKE '/api/gb28181/security/access-rules%' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
END $$;
