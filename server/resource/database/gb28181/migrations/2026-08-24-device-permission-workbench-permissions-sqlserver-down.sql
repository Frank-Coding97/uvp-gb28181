-- 设备权限工作台 API 权限回滚(SQL Server,可重复执行)。
DELETE FROM [sys_casbin_rule] WHERE [v1] IN (N'/api/gb28181/device-mgmt/permission-workbench/summary',N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve',N'/api/gb28181/device-mgmt/permission-workbench/grants/query',N'/api/gb28181/device-mgmt/permission-workbench/grant-targets',N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments',N'/api/gb28181/device-mgmt/permission-workbench/grants/apply');
DELETE ma FROM [sys_menu_api] ma
JOIN [sys_menu] m ON m.[id]=ma.[menu_id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[name] IN (N'device-assignment',N'device-assignment-assign',N'device-assignment-share')
  AND a.[path] IN (N'/api/gb28181/device-mgmt/permission-workbench/summary',N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve',N'/api/gb28181/device-mgmt/permission-workbench/grants/query',N'/api/gb28181/device-mgmt/permission-workbench/grant-targets',N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments',N'/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE [sys_api] SET [deleted_at]=GETDATE(), [updated_at]=GETDATE()
WHERE [deleted_at] IS NULL AND [path] IN (N'/api/gb28181/device-mgmt/permission-workbench/summary',N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve',N'/api/gb28181/device-mgmt/permission-workbench/grants/query',N'/api/gb28181/device-mgmt/permission-workbench/grant-targets',N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments',N'/api/gb28181/device-mgmt/permission-workbench/grants/apply');
UPDATE [sys_menu] SET [title]=N'设备分配', [updated_at]=GETDATE()
WHERE [name]=N'device-assignment' AND [title]=N'设备权限工作台' AND [deleted_at] IS NULL;
