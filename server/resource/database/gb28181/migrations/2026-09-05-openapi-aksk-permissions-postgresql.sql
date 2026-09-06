-- openapi-aksk-permissions:begin
-- T04 management permission catalog. Permission and API IDs are resolved by
-- natural keys; only the existing system-admin role receives the initial grant.
-- Ordinary roles and the guest role must be granted explicitly by an operator.

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 客户端列表','/api/gb28181/openapi-clients','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '创建 OpenAPI 客户端','/api/gb28181/openapi-clients','POST','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 能力目录','/api/gb28181/openapi-clients/capabilities','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/capabilities' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 客户端详情','/api/gb28181/openapi-clients/:id','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '分配 OpenAPI 客户端能力','/api/gb28181/openapi-clients/:id/scopes','PUT','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/scopes' AND method='PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '轮换 OpenAPI 客户端密钥','/api/gb28181/openapi-clients/:id/rotate-secret','POST','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/rotate-secret' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '启用 OpenAPI 客户端','/api/gb28181/openapi-clients/:id/enable','POST','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/enable' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '停用 OpenAPI 客户端','/api/gb28181/openapi-clients/:id/disable','POST','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/disable' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '撤销 OpenAPI 客户端','/api/gb28181/openapi-clients/:id/revoke','POST','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/revoke' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 客户端审计','/api/gb28181/openapi-clients/:id/audits','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/audits' AND method='GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 客户端撤销进度','/api/gb28181/openapi-clients/:id/revocation-status','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/:id/revocation-status' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_read','','查看 OpenAPI 客户端',1,0,100,3,'gb28181:openapi:client:read','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:read' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_create','','创建 OpenAPI 客户端',1,0,100,3,'gb28181:openapi:client:create','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:create' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_grant','','分配 OpenAPI 客户端能力',1,0,100,3,'gb28181:openapi:client:grant','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:grant' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_rotate','','轮换 OpenAPI 客户端密钥',1,0,100,3,'gb28181:openapi:client:rotate','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:rotate' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_status','','启停或撤销 OpenAPI 客户端',1,0,100,3,'gb28181:openapi:client:status','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:status' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'','Permission_gb28181_openapi_client_audit','','查看 OpenAPI 客户端审计',1,0,100,3,'gb28181:openapi:client:audit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:openapi:client:audit' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id)
SELECT r.id,m.id
FROM sys_role r CROSS JOIN sys_menu m
WHERE r.name='系统管理员' AND r.deleted_at IS NULL
  AND m.permission IN ('gb28181:openapi:client:read','gb28181:openapi:client:create','gb28181:openapi:client:grant','gb28181:openapi:client:rotate','gb28181:openapi:client:status','gb28181:openapi:client:audit')
  AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.id AND x.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:read' AND m.deleted_at IS NULL
  AND a.deleted_at IS NULL
  AND ((a.path='/api/gb28181/openapi-clients' AND a.method='GET') OR (a.path='/api/gb28181/openapi-clients/capabilities' AND a.method='GET') OR (a.path='/api/gb28181/openapi-clients/:id' AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:create' AND a.path='/api/gb28181/openapi-clients' AND a.method='POST' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:grant' AND a.path='/api/gb28181/openapi-clients/:id/scopes' AND a.method='PUT' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:rotate' AND a.path='/api/gb28181/openapi-clients/:id/rotate-secret' AND a.method='POST' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:status' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND ((a.path='/api/gb28181/openapi-clients/:id/enable' AND a.method='POST') OR (a.path='/api/gb28181/openapi-clients/:id/disable' AND a.method='POST') OR (a.path='/api/gb28181/openapi-clients/:id/revoke' AND a.method='POST') OR (a.path='/api/gb28181/openapi-clients/:id/revocation-status' AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:audit' AND a.path='/api/gb28181/openapi-clients/:id/audits' AND a.method='GET' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE rm.role_id IN (SELECT id FROM sys_role WHERE name='系统管理员' AND deleted_at IS NULL)
  AND m.permission LIKE 'gb28181:openapi:client:%'
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- openapi-aksk-permissions:end
