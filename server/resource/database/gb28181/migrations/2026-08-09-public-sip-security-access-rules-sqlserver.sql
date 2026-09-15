-- Manual SIP blacklist/allowlist rules and protected APIs (SQL Server)
IF OBJECT_ID(N'gb_sip_security_access_rule', N'U') IS NULL
BEGIN
CREATE TABLE gb_sip_security_access_rule (
 id BIGINT IDENTITY(1,1) PRIMARY KEY, list_type NVARCHAR(16) NOT NULL, match_type NVARCHAR(16) NOT NULL,
 match_value NVARCHAR(255) NOT NULL, scope NVARCHAR(32) NOT NULL DEFAULT 'all_sip', status NVARCHAR(16) NOT NULL DEFAULT 'enabled',
 expires_at DATETIME2 NULL, note NVARCHAR(255) NOT NULL DEFAULT '', created_by NVARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME2 NOT NULL, updated_at DATETIME2 NOT NULL,
 CONSTRAINT uk_gb_sip_security_access_rule_match UNIQUE(list_type,match_type,match_value));
CREATE INDEX idx_gb_sip_security_access_rule_list_status ON gb_sip_security_access_rule(list_type,status);
END;
MERGE sys_api AS target USING (VALUES
 (N'查看安全访问名单','/api/gb28181/security/access-rules','GET'),
 (N'创建安全访问规则','/api/gb28181/security/access-rules','POST'),
 (N'修改安全访问规则','/api/gb28181/security/access-rules/:id','PUT'),
 (N'删除安全访问规则','/api/gb28181/security/access-rules/:id','DELETE')) source(title,path,method)
ON target.path=source.path AND target.method=source.method AND target.deleted_at IS NULL
WHEN NOT MATCHED THEN INSERT(title,path,method,api_group,created_at,updated_at,created_by) VALUES(source.title,source.path,source.method,N'GB28181 接入安全',SYSUTCDATETIME(),SYSUTCDATETIME(),1);
DECLARE @SECURITY_MENU_ID BIGINT;
SELECT TOP 1 @SECURITY_MENU_ID=id FROM sys_menu WHERE path='/gb28181/security' AND deleted_at IS NULL ORDER BY id;
INSERT INTO sys_menu_api(menu_id,api_id) SELECT @SECURITY_MENU_ID,a.id FROM sys_api a WHERE @SECURITY_MENU_ID IS NOT NULL AND a.path LIKE '/api/gb28181/security/access-rules%' AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=@SECURITY_MENU_ID AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path LIKE '/api/gb28181/security/access-rules%' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
