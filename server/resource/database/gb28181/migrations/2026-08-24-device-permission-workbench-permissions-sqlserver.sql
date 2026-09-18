-- 设备权限工作台 API 权限迁移(SQL Server,幂等)。

UPDATE [sys_menu] SET [title]=N'设备权限工作台', [updated_at]=GETDATE()
WHERE [name]=N'device-assignment' AND [deleted_at] IS NULL;

UPDATE [sys_api] SET [deleted_at]=GETDATE(), [updated_at]=GETDATE()
WHERE [deleted_at] IS NULL AND [path] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE ma FROM [sys_menu_api] ma JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE a.[path] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM [sys_casbin_rule] WHERE [v1] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');

UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询设备权限汇总',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND [method]=N'GET';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询设备权限汇总',N'/api/gb28181/device-mgmt/permission-workbench/summary',N'GET',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND [method]=N'GET' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'解析工作台设备',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'解析工作台设备',N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询设备共享授权',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询设备共享授权',N'/api/gb28181/device-mgmt/permission-workbench/grants/query',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询共享目标',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND [method]=N'GET';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询共享目标',N'/api/gb28181/device-mgmt/permission-workbench/grant-targets',N'GET',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND [method]=N'GET' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'调整设备归属',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'调整设备归属',N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'整部门调整设备归属',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'整部门调整设备归属',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'应用设备共享授权',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'应用设备共享授权',N'/api/gb28181/device-mgmt/permission-workbench/grants/apply',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND [method]=N'POST' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[name]=N'device-assignment' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
  AND ((a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND a.[method]=N'GET') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.[method]=N'POST') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.[method]=N'POST') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.[method]=N'GET'))
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[permission]=N'gb28181:device:assign' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[path] IN (N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments') AND a.[method]=N'POST'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[permission]=N'gb28181:device:share' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.[method]=N'POST'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p',CONCAT('role_',rm.[role_id]),a.[path],a.[method],'*','',''
FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id]
JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[name] IN (N'device-assignment',N'device-assignment-assign',N'device-assignment-share') AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
  AND a.[path] LIKE N'/api/gb28181/device-mgmt/permission-workbench/%'
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]=CONCAT('role_',rm.[role_id]) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
