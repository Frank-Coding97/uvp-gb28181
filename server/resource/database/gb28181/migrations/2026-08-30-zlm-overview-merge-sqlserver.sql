-- zlm-overview-merge:start
-- Merge cluster and runtime overview into one visible menu (SQL Server).
UPDATE [sys_menu] SET [component]='gb28181/zlm/ClusterOverview',[title]=N'总览',[icon]='lucide:LayoutDashboard',[sort]=10,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [component]='gb28181/zlm/RuntimeOverview',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT rm.[role_id],o.[id] FROM [sys_role_menu] rm
JOIN [sys_menu] r ON r.[id]=rm.[menu_id] AND r.[path]='/gb28181/zlm/runtime' AND r.[deleted_at] IS NULL
CROSS JOIN [sys_menu] o
WHERE o.[path]='/gb28181/zlm/overview' AND o.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=rm.[role_id] AND x.[menu_id]=o.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT o.[id],a.[id] FROM [sys_menu] o CROSS JOIN [sys_api] a
WHERE o.[path]='/gb28181/zlm/overview' AND o.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[method]='GET'
AND a.[path] IN ('/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime')
AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] ma WHERE ma.[menu_id]=o.[id] AND ma.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p','role_'+CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id]
JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[path]='/gb28181/zlm/overview' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
AND a.[path] IN ('/api/gb28181/zlm/overview','/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime')
AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_'+CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
-- zlm-overview-merge:end
