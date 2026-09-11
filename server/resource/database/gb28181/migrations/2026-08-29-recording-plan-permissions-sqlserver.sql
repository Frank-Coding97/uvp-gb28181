-- Recording plan menus and API permissions (SQL Server, idempotent).
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[icon],[sort],[created_at],[updated_at],[created_by])
SELECT 0,'/gb28181/recording-schedules','gb28181-recording-schedules','gb28181/recording-schedules/index',N'录像计划',2,'gb28181:recording-plan:view','lucide:CalendarClock',34,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:view' AND [deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'维护录像计划',3,'gb28181:recording-plan:maintain',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[permission]='gb28181:recording-plan:view' AND p.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:maintain' AND [deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'分配录像计划',3,'gb28181:recording-plan:assign',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[permission]='gb28181:recording-plan:view' AND p.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:assign' AND [deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT s.[title],s.[path],s.[method],N'GB28181 录像计划',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 (N'查询录像计划','/api/gb28181/recording-plans','GET'),(N'新建录像计划','/api/gb28181/recording-plans','POST'),
 (N'查看录像计划','/api/gb28181/recording-plans/:id','GET'),(N'编辑录像计划','/api/gb28181/recording-plans/:id','PUT'),
 (N'删除录像计划','/api/gb28181/recording-plans/:id','DELETE'),(N'启停录像计划','/api/gb28181/recording-plans/:id/status','PATCH'),
 (N'搜索分配设备','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
 (N'搜索分配通道','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
 (N'分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST'),
 (N'切换通道录像模式','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
 (N'查询计划通道状态','/api/gb28181/recording-plans/:id/channels','GET'),
 (N'诊断通道录像','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
 (N'查询通道执行时间线','/api/gb28181/recording-plans/channels/:channelId/timeline','GET')
) s([title],[path],[method]) WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=s.[path] AND a.[method]=s.[method] AND a.[deleted_at] IS NULL);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:view' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[method]='GET' AND a.[path] IN ('/api/gb28181/recording-plans','/api/gb28181/recording-plans/:id','/api/gb28181/recording-plans/:id/channels','/api/gb28181/recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/timeline')
AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:maintain' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
AND ((a.[path]='/api/gb28181/recording-plans' AND a.[method]='POST') OR (a.[path]='/api/gb28181/recording-plans/:id' AND a.[method] IN ('PUT','DELETE')) OR (a.[path]='/api/gb28181/recording-plans/:id/status' AND a.[method]='PATCH'))
AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:assign' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
AND a.[path] IN ('/api/gb28181/recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignments','/api/gb28181/recording-plans/channels/:channelId/recording-mode')
AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p','role_'+CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id] JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_'+CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
