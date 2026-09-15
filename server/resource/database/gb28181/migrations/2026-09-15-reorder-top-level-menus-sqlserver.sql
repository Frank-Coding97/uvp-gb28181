-- 重排「一级菜单」（parent_id=0）的侧栏展示顺序，统一 sort 值（SQL Server 方言）。
-- 依据与目标顺序见 2026-09-15-reorder-top-level-menus.sql 的文件头注释。
-- 定位一律用 path：menu_id 跨环境会漂移。
UPDATE [sys_menu] SET [sort]=10, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/home' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>10);
UPDATE [sys_menu] SET [sort]=20, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path] IN (N'/gb28181/device-mgmt/index',N'/gb28181/device-mgmt') AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>20);
UPDATE [sys_menu] SET [sort]=30, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/multi-screen-playback' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>30);
UPDATE [sys_menu] SET [sort]=40, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/alarm-management' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>40);
UPDATE [sys_menu] SET [sort]=50, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/cloud-recordings' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>50);
UPDATE [sys_menu] SET [sort]=60, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/recording-schedules' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>60);
UPDATE [sys_menu] SET [sort]=70, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/cascade' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>70);
UPDATE [sys_menu] SET [sort]=80, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/sip/platform' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>80);
UPDATE [sys_menu] SET [sort]=90, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/sip-traces' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>90);
UPDATE [sys_menu] SET [sort]=100, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/sip/config' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>100);
UPDATE [sys_menu] SET [sort]=110, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/security-preview' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>110);
UPDATE [sys_menu] SET [sort]=120, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/device-assignment' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>120);
UPDATE [sys_menu] SET [sort]=130, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/openapi-client' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>130);
UPDATE [sys_menu] SET [sort]=140, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/media' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>140);
UPDATE [sys_menu] SET [sort]=150, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/system' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>150);
UPDATE [sys_menu] SET [sort]=160, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/sysjobs' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>160);
UPDATE [sys_menu] SET [sort]=170, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/demo' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>170);
UPDATE [sys_menu] SET [sort]=180, [updated_at]=CURRENT_TIMESTAMP WHERE ([parent_id]=0 OR [parent_id] IS NULL) AND [path]=N'/gb28181/device-record-playback/:channelId' AND [deleted_at] IS NULL AND ([sort] IS NULL OR [sort]<>180);
