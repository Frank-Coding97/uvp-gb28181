-- drop-dup-security-menu:down:start
-- Restore the duplicate "GB28181 access security" menu, its role binding and menu-API bindings (SQL Server).
-- The frontend page was deleted together with the menu, so restoring the menu also
-- requires restoring web/src/views/gb28181/security/index.vue from git.
IF NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [id]=140372) INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[sort],[type],[hide],[disable],[keep_alive],[icon],[created_by],[created_at],[updated_at]) VALUES (140372,0,N'/gb28181/security',N'gb28181-security',N'gb28181/security/index',N'国标接入安全',5,2,0,1,0,N'lucide:ShieldCheck',1,GETDATE(),GETDATE());
IF NOT EXISTS (SELECT 1 FROM [sys_role_menu] WHERE [role_id]=1 AND [menu_id]=140372) INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140372);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON a.[path] LIKE N'/api/gb28181/security/%' AND a.[deleted_at] IS NULL WHERE m.[path]=N'/gb28181/security' AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
-- drop-dup-security-menu:down:end
