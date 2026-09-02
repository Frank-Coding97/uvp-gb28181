-- zlm-single-menu-workbench:down:start
-- Restore the direct ZLM navigation introduced by V3 (SQL Server).
DECLARE @media_menu_id BIGINT;
SELECT @media_menu_id=MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [redirect]='/gb28181/zlm/overview',[component]='',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[type]=1,[hide]=0,[updated_at]=GETDATE() WHERE [path]='/media' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=GETDATE() WHERE [path] IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling','/media/nodes/:id') AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/ClusterOverview',[title]=N'集群总览',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/NodeList',[title]=N'节点管理',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/RuntimeOverview',[title]=N'总览',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/StreamManagement',[title]=N'流管理',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/SessionManagement',[title]=N'会话管理',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/ProxyManagement',[title]=N'拉流/推流代理',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/FFmpegSources',[title]=N'FFmpeg 源',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/RTPServices',[title]=N'RTP 服务',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/ServerConfig',[title]=N'服务器配置',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/SchedulerStrategy',[title]=N'调度策略',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@media_menu_id,[component]='gb28181/zlm/SchedulerLog',[title]=N'调度日志',[hide]=0,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=0,[component]='gb28181/zlm/NodeDetail',[title]=N'节点详情',[hide]=1,[updated_at]=GETDATE() WHERE [path]='/gb28181/zlm/nodes/:id' AND [deleted_at] IS NULL;
-- zlm-single-menu-workbench:down:end
