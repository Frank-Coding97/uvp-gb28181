-- zlm-global-sidebar-menu:start
-- Display the five canonical ZLM workspaces under the global media sidebar (SQL Server).
DECLARE @media_menu_id BIGINT;
SELECT @media_menu_id=MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [redirect]='/media/overview',[component]='',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[sort]=9,[type]=1,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/workbench/MediaOverview',[title]=N'运行总览',[sort]=10,[type]=2,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/overview' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/workbench/MediaMonitoring',[title]=N'流与会话',[sort]=20,[type]=2,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/monitoring' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/workbench/IngressManagement',[title]=N'接入管理',[sort]=30,[type]=2,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/ingress' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/workbench/NodeManagement',[title]=N'节点管理',[sort]=40,[type]=2,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/nodes' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/workbench/SchedulingManagement',[title]=N'调度管理',[sort]=50,[type]=2,[hide]=0,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/scheduling' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media/nodes' AND [deleted_at] IS NULL),[component]='gb28181/zlm/workbench/nodes/NodeDetail',[title]=N'节点详情',[hide]=1,[keep_alive]=1,[updated_at]=GETDATE() WHERE [path]='/media/nodes/:id' AND [deleted_at] IS NULL;
-- zlm-global-sidebar-menu:end
