-- Manual SIP blacklist/allowlist rules and protected APIs (MySQL)
CREATE TABLE IF NOT EXISTS `gb_sip_security_access_rule` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `list_type` VARCHAR(16) NOT NULL,
  `match_type` VARCHAR(16) NOT NULL,
  `match_value` VARCHAR(255) NOT NULL,
  `scope` VARCHAR(32) NOT NULL DEFAULT 'all_sip',
  `status` VARCHAR(16) NOT NULL DEFAULT 'enabled',
  `expires_at` DATETIME NULL,
  `note` VARCHAR(255) NOT NULL DEFAULT '',
  `created_by` VARCHAR(64) NOT NULL DEFAULT '',
  `created_at` DATETIME NOT NULL,
  `updated_at` DATETIME NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_sip_security_access_rule_match` (`list_type`,`match_type`,`match_value`),
  KEY `idx_gb_sip_security_access_rule_list_status` (`list_type`,`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO `sys_api` (`title`,`path`,`method`,`api_group`,`created_at`,`updated_at`,`created_by`)
SELECT v.title,v.path,v.method,'GB28181 接入安全',NOW(),NOW(),1
FROM (SELECT '查看安全访问名单' title,'/api/gb28181/security/access-rules' path,'GET' method UNION ALL
      SELECT '创建安全访问规则','/api/gb28181/security/access-rules','POST' UNION ALL
      SELECT '修改安全访问规则','/api/gb28181/security/access-rules/:id','PUT' UNION ALL
      SELECT '删除安全访问规则','/api/gb28181/security/access-rules/:id','DELETE') v
WHERE NOT EXISTS (SELECT 1 FROM `sys_api` a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
SET @SECURITY_MENU_ID := (SELECT id FROM sys_menu WHERE path='/gb28181/security' AND deleted_at IS NULL ORDER BY id LIMIT 1);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT @SECURITY_MENU_ID,a.id FROM sys_api a
WHERE @SECURITY_MENU_ID IS NOT NULL AND a.path LIKE '/api/gb28181/security/access-rules%'
AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=@SECURITY_MENU_ID AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a
WHERE a.path LIKE '/api/gb28181/security/access-rules%'
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
