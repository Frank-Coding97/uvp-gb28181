-- Alarm management menu, permissions, APIs and query index (SQL Server 2017+).
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_alarm_event') AND name=N'idx_alarm_time')
  CREATE INDEX [idx_alarm_time] ON [gb_alarm_event] ([alarm_time]);

UPDATE [sys_menu] SET [name]='gb28181-alarm-management',[component]='gb28181/alarm-management/index',[title]=N'告警管理',[icon]='lucide:BellRing',[type]=2,[permission]='gb28181:alarm:view',[hide]=0,[disable]=0,[updated_at]=CURRENT_TIMESTAMP,[deleted_at]=NULL
WHERE [path]='/gb28181/alarm-management';

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[icon],[sort],[type],[permission],[hide],[disable],[created_at],[updated_at],[created_by])
SELECT dm.[parent_id],'/gb28181/alarm-management','gb28181-alarm-management','gb28181/alarm-management/index',N'告警管理','lucide:BellRing',30,2,'gb28181:alarm:view',0,0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM [sys_menu] dm
WHERE dm.[id]=(SELECT TOP 1 [id] FROM [sys_menu] WHERE [path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND [deleted_at] IS NULL ORDER BY CASE WHEN [path]='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,[id])
  AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/alarm-management' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT m.[id],'','','',N'物理删除告警',3,'gb28181:alarm:delete',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM [sys_menu] m WHERE m.[path]='/gb28181/alarm-management' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:alarm:delete' AND [deleted_at] IS NULL);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 告警管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM (VALUES
 (N'查询告警列表','/api/gb28181/alarms','GET'),
 (N'查询告警详情','/api/gb28181/alarms/:id','GET'),
 (N'物理删除单条告警','/api/gb28181/alarms/:id','DELETE'),
 (N'批量物理删除告警','/api/gb28181/alarms/batch-delete','POST')
) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT source_role.[role_id],alarm_menu.[id] FROM [sys_role_menu] source_role
JOIN [sys_menu] device_menu ON device_menu.[id]=source_role.[menu_id]
CROSS JOIN [sys_menu] alarm_menu
WHERE device_menu.[path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND device_menu.[deleted_at] IS NULL
  AND alarm_menu.[path]='/gb28181/alarm-management' AND alarm_menu.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=source_role.[role_id] AND existing.[menu_id]=alarm_menu.[id]);
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:alarm:delete' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=1 AND existing.[menu_id]=m.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/alarm-management' AND m.[deleted_at] IS NULL
  AND a.[path] IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id') AND a.[method]='GET'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[permission]='gb28181:alarm:delete' AND m.[deleted_at] IS NULL
  AND ((a.[path]='/api/gb28181/alarms/:id' AND a.[method]='DELETE') OR (a.[path]='/api/gb28181/alarms/batch-delete' AND a.[method]='POST'))
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_' + CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id] CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/alarm-management' AND a.[path] IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id') AND a.[method]='GET'
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_' + CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a
WHERE ((a.[path]='/api/gb28181/alarms/:id' AND a.[method]='DELETE') OR (a.[path]='/api/gb28181/alarms/batch-delete' AND a.[method]='POST'))
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
