-- openapi-aksk-permissions:begin
-- T04 management permission catalog. Permission and API IDs are resolved by
-- natural keys; only the existing system-admin role receives the initial grant.
-- Ordinary roles and the guest role must be granted explicitly by an operator.

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查看 OpenAPI 客户端列表',N'/api/gb28181/openapi-clients',N'GET',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients' AND method=N'GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'创建 OpenAPI 客户端',N'/api/gb28181/openapi-clients',N'POST',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查看 OpenAPI 能力目录',N'/api/gb28181/openapi-clients/capabilities',N'GET',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/capabilities' AND method=N'GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查看 OpenAPI 客户端详情',N'/api/gb28181/openapi-clients/:id',N'GET',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id' AND method=N'GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'分配 OpenAPI 客户端能力',N'/api/gb28181/openapi-clients/:id/scopes',N'PUT',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/scopes' AND method=N'PUT' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'轮换 OpenAPI 客户端密钥',N'/api/gb28181/openapi-clients/:id/rotate-secret',N'POST',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/rotate-secret' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'启用 OpenAPI 客户端',N'/api/gb28181/openapi-clients/:id/enable',N'POST',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/enable' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'停用 OpenAPI 客户端',N'/api/gb28181/openapi-clients/:id/disable',N'POST',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/disable' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'撤销 OpenAPI 客户端',N'/api/gb28181/openapi-clients/:id/revoke',N'POST',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/revoke' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查看 OpenAPI 客户端审计',N'/api/gb28181/openapi-clients/:id/audits',N'GET',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/audits' AND method=N'GET' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'查看 OpenAPI 客户端撤销进度',N'/api/gb28181/openapi-clients/:id/revocation-status',N'GET',N'GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/openapi-clients/:id/revocation-status' AND method=N'GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_read',N'',N'查看 OpenAPI 客户端',1,0,100,3,N'gb28181:openapi:client:read',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:read' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_create',N'',N'创建 OpenAPI 客户端',1,0,100,3,N'gb28181:openapi:client:create',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:create' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_grant',N'',N'分配 OpenAPI 客户端能力',1,0,100,3,N'gb28181:openapi:client:grant',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:grant' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_rotate',N'',N'轮换 OpenAPI 客户端密钥',1,0,100,3,N'gb28181:openapi:client:rotate',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:rotate' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_status',N'',N'启停或撤销 OpenAPI 客户端',1,0,100,3,N'gb28181:openapi:client:status',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:status' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,N'',N'Permission_gb28181_openapi_client_audit',N'',N'查看 OpenAPI 客户端审计',1,0,100,3,N'gb28181:openapi:client:audit',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission=N'gb28181:openapi:client:audit' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id)
SELECT r.id,m.id
FROM sys_role r CROSS JOIN sys_menu m
WHERE r.name=N'系统管理员' AND r.deleted_at IS NULL
  AND m.permission IN (N'gb28181:openapi:client:read',N'gb28181:openapi:client:create',N'gb28181:openapi:client:grant',N'gb28181:openapi:client:rotate',N'gb28181:openapi:client:status',N'gb28181:openapi:client:audit')
  AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.id AND x.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id
FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:read' AND m.deleted_at IS NULL
  AND a.deleted_at IS NULL
  AND ((a.path=N'/api/gb28181/openapi-clients' AND a.method=N'GET') OR (a.path=N'/api/gb28181/openapi-clients/capabilities' AND a.method=N'GET') OR (a.path=N'/api/gb28181/openapi-clients/:id' AND a.method=N'GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:create' AND a.path=N'/api/gb28181/openapi-clients' AND a.method=N'POST' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:grant' AND a.path=N'/api/gb28181/openapi-clients/:id/scopes' AND a.method=N'PUT' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:rotate' AND a.path=N'/api/gb28181/openapi-clients/:id/rotate-secret' AND a.method=N'POST' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:status' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND ((a.path=N'/api/gb28181/openapi-clients/:id/enable' AND a.method=N'POST') OR (a.path=N'/api/gb28181/openapi-clients/:id/disable' AND a.method=N'POST') OR (a.path=N'/api/gb28181/openapi-clients/:id/revoke' AND a.method=N'POST') OR (a.path=N'/api/gb28181/openapi-clients/:id/revocation-status' AND a.method=N'GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:openapi:client:audit' AND a.path=N'/api/gb28181/openapi-clients/:id/audits' AND a.method=N'GET' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT N'p',CONCAT(N'role_',rm.role_id),a.path,a.method,N'*',N'',N''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE rm.role_id IN (SELECT id FROM sys_role WHERE name=N'系统管理员' AND deleted_at IS NULL)
  AND m.permission LIKE N'gb28181:openapi:client:%'
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype=N'p' AND c.v0=CONCAT(N'role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3=N'*');

-- openapi-aksk-permissions:end
