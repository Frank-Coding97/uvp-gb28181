-- OpenAPI 分组能力目录管理权限（MySQL）。能力目录仍由显式 registry 发布，sys_api 仅提供标题/分组资产。
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查看 OpenAPI 分组能力目录','/api/gb28181/openapi-clients/capabilities/catalog','GET','GB28181 OpenAPI 客户端',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/openapi-clients/capabilities/catalog' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission='gb28181:openapi:client:read' AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/openapi-clients/capabilities/catalog' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),'/api/gb28181/openapi-clients/capabilities/catalog','GET','*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:openapi:client:read' AND m.deleted_at IS NULL
  AND a.path='/api/gb28181/openapi-clients/capabilities/catalog' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1='/api/gb28181/openapi-clients/capabilities/catalog' AND p.v2='GET' AND p.v3='*');
