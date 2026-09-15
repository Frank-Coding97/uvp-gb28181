-- Playback scheme APIs and one hidden permission point. SQL Server.
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 (N'查询播放方案','/api/gb28181/playback-schemes','GET'),
 (N'查看播放方案','/api/gb28181/playback-schemes/:id','GET'),
 (N'创建播放方案','/api/gb28181/playback-schemes','POST'),
 (N'重命名播放方案','/api/gb28181/playback-schemes/:id','PATCH'),
 (N'覆盖播放方案','/api/gb28181/playback-schemes/:id/layout','PUT'),
 (N'删除播放方案','/api/gb28181/playback-schemes/:id','DELETE')) v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by]) SELECT 140355,'','',N'管理播放方案',3,'gb28181:playback-scheme:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:playback-scheme:manage' AND [deleted_at] IS NULL);
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:playback-scheme:manage' AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:playback-scheme:manage' AND a.[path] LIKE '/api/gb28181/playback-schemes%' AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5]) SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a WHERE a.[path] LIKE '/api/gb28181/playback-schemes%' AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] x WHERE x.[ptype]='p' AND x.[v0]='role_1' AND x.[v1]=a.[path] AND x.[v2]=a.[method] AND x.[v3]='*');
