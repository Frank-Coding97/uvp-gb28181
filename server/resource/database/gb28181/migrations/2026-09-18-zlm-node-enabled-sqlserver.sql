-- Split operator admission intent from ZLM heartbeat health (SQL Server 2017+).
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND COL_LENGTH(N'meta_node', N'enabled') IS NULL
  ALTER TABLE [meta_node] ADD [enabled] BIT NOT NULL CONSTRAINT [df_meta_node_enabled] DEFAULT 1 WITH VALUES;
UPDATE [meta_node] SET [enabled]=0,[state]='offline' WHERE [state]='maintenance';
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name=N'idx_enabled' AND object_id=OBJECT_ID(N'meta_node'))
  CREATE INDEX [idx_enabled] ON [meta_node] ([enabled]);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'启用媒体节点',N'/api/gb28181/zlm/nodes/:id/enable',N'POST',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/zlm/nodes/:id/enable' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT N'停用媒体节点',N'/api/gb28181/zlm/nodes/:id/disable',N'POST',N'按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path=N'/api/gb28181/zlm/nodes/:id/disable' AND method=N'POST' AND deleted_at IS NULL);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.permission=N'gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path IN (N'/api/gb28181/zlm/nodes/:id/enable',N'/api/gb28181/zlm/nodes/:id/disable')
  AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission=N'gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL
  AND a.path IN (N'/api/gb28181/zlm/nodes/:id/enable',N'/api/gb28181/zlm/nodes/:id/disable')
  AND a.method=N'POST' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',rm.role_id) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
