-- 作业单菜单与权限（SQL Server，幂等）。
-- 作业单是录制的唯一入口：先填作业单、校验通过后才开始录制。
-- 组件路径对应 web/src/views/gb28181/work-orders/index.vue。
-- GB28181 菜单为根级平铺（parent_id = 0），排序接在 cascade(13) 之后。

-- 1) 菜单
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[created_by])
SELECT 0,'/gb28181/work-orders',N'gb28181-work-orders',N'gb28181/work-orders/index',N'作业单',0,0,14,2,N'',N'lucide:ClipboardList',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/work-orders' AND [deleted_at] IS NULL);

-- 2) 按钮权限
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[created_by])
SELECT COALESCE((SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/gb28181/work-orders' AND [type]=2 AND [deleted_at] IS NULL),0),N'',N'Permission_gb28181_work_order_create',N'',N'新建作业单',1,0,1,3,N'gb28181:work-order:create',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]=N'gb28181:work-order:create' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[created_by])
SELECT COALESCE((SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/gb28181/work-orders' AND [type]=2 AND [deleted_at] IS NULL),0),N'',N'Permission_gb28181_work_order_stop',N'',N'结束作业单录制',1,0,2,3,N'gb28181:work-order:stop',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]=N'gb28181:work-order:stop' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[created_by])
SELECT COALESCE((SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/gb28181/work-orders' AND [type]=2 AND [deleted_at] IS NULL),0),N'',N'Permission_gb28181_work_order_view',N'',N'查看作业单',1,0,3,3,N'gb28181:work-order:view',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]=N'gb28181:work-order:view' AND [deleted_at] IS NULL);

-- 3) 角色绑定（菜单本身 + 三个按钮权限）
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m
WHERE m.[deleted_at] IS NULL
  AND (m.[path]='/gb28181/work-orders' OR m.[permission] IN (N'gb28181:work-order:create',N'gb28181:work-order:stop',N'gb28181:work-order:view'))
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);

-- 4) API 权限
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT s.[title],s.[path],s.[method],N'GB28181 作业单',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT N'新建作业单' AS [title],'/api/gb28181/work-orders' AS [path],'POST' AS [method]
 UNION ALL SELECT N'作业单列表','/api/gb28181/work-orders','GET'
 UNION ALL SELECT N'进行中作业单','/api/gb28181/work-orders/active','GET'
 UNION ALL SELECT N'作业单详情','/api/gb28181/work-orders/:id','GET'
 UNION ALL SELECT N'结束作业单录制','/api/gb28181/work-orders/:id/stop','POST'
 UNION ALL SELECT N'下载作业单录像','/api/gb28181/work-orders/:id/download','GET'
 UNION ALL SELECT N'播放作业单录像分片','/api/gb28181/work-orders/:id/files/:fileId','GET'
) s
WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=s.[path] AND a.[method]=s.[method] AND a.[deleted_at] IS NULL);

-- 5) 菜单与 API 的精确绑定
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON a.[deleted_at] IS NULL
WHERE m.[permission]=N'gb28181:work-order:view' AND m.[deleted_at] IS NULL
  AND a.[method]='GET' AND a.[path] IN ('/api/gb28181/work-orders','/api/gb28181/work-orders/active','/api/gb28181/work-orders/:id','/api/gb28181/work-orders/:id/download','/api/gb28181/work-orders/:id/files/:fileId')
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON a.[deleted_at] IS NULL
WHERE m.[permission]=N'gb28181:work-order:create' AND m.[deleted_at] IS NULL
  AND a.[method]='POST' AND a.[path]='/api/gb28181/work-orders'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON a.[deleted_at] IS NULL
WHERE m.[permission]=N'gb28181:work-order:stop' AND m.[deleted_at] IS NULL
  AND a.[method]='POST' AND a.[path]='/api/gb28181/work-orders/:id/stop'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

-- 6) Casbin 规则（同一角色可能通过菜单与按钮命中同一 API，写入前必须去重）
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p',CONCAT(N'role_',rm.[role_id]),a.[path],a.[method],N'*',N'',N''
FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id]
JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE (m.[path]='/gb28181/work-orders' OR m.[permission] IN (N'gb28181:work-order:create',N'gb28181:work-order:stop',N'gb28181:work-order:view'))
  AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]=N'p' AND c.[v0]=CONCAT(N'role_',rm.[role_id]) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]=N'*');
