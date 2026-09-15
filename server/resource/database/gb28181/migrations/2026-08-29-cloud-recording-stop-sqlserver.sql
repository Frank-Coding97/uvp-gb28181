-- Cloud recording stop permission (SQL Server).
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT m.[id],'','','',N'停止录像',3,'gb28181:recording:stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
FROM [sys_menu] m WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:stop' AND [deleted_at] IS NULL);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT N'停止云端录像','/api/gb28181/cloud-recordings/active/:id/stop','POST',N'GB28181 云端录像控制',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]='/api/gb28181/cloud-recordings/active/:id/stop' AND [method]='POST' AND [deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:recording:stop' AND m.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[permission]='gb28181:recording:stop' AND m.[deleted_at] IS NULL
  AND a.[path]='/api/gb28181/cloud-recordings/active/:id/stop' AND a.[method]='POST' AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a
WHERE a.[path]='/api/gb28181/cloud-recordings/active/:id/stop' AND a.[method]='POST' AND a.[deleted_at] IS NULL
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
