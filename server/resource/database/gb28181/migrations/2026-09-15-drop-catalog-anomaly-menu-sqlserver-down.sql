-- drop-catalog-anomaly-menu:down:start
-- Restore the "catalog anomaly" menu row and its role binding (SQL Server).
-- The frontend page was deleted together with the menu, so restoring the menu also
-- requires restoring web/src/views/gb28181/device-mgmt/anomaly/index.vue from git.
IF NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [id]=140357) INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[sort],[type],[hide],[disable],[keep_alive],[icon],[created_by],[created_at],[updated_at]) VALUES (140357,0,N'/gb28181/device-mgmt/anomaly',N'device-mgmt-anomaly',N'gb28181/device-mgmt/anomaly/index',N'目录异常',6,2,1,0,0,N'lucide:Cctv',0,GETDATE(),GETDATE());
IF NOT EXISTS (SELECT 1 FROM [sys_role_menu] WHERE [role_id]=1 AND [menu_id]=140357) INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140357);
-- drop-catalog-anomaly-menu:down:end
