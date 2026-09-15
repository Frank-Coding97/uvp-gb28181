-- media-workbench-v2:down:start
-- Remove only V2-owned navigation rows; legacy menu/API/button/Casbin anchors remain intact (SQL Server).
DELETE rm FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] WHERE m.[path] IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND m.[deleted_at] IS NULL;
DELETE FROM [sys_menu] WHERE [path] IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=0,[name]='Media',[redirect]='/gb28181/zlm/overview',[component]='',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[sort]=9,[type]=1,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-overview',[redirect]='',[component]='gb28181/zlm/ClusterOverview',[title]=N'集群概览',[icon]='lucide:LayoutDashboard',[sort]=10,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-nodes',[redirect]='',[component]='gb28181/zlm/NodeList',[title]=N'节点管理',[icon]='lucide:Server',[sort]=11,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-scheduler-strategy',[redirect]='',[component]='gb28181/zlm/SchedulerStrategy',[title]=N'调度策略',[icon]='lucide:Workflow',[sort]=12,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-scheduler-log',[redirect]='',[component]='gb28181/zlm/SchedulerLog',[title]=N'调度日志',[icon]='lucide:History',[sort]=13,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-runtime',[redirect]='',[component]='gb28181/zlm/RuntimeOverview',[title]=N'运行监控',[icon]='lucide:Activity',[sort]=20,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-streams',[redirect]='',[component]='gb28181/zlm/StreamManagement',[title]=N'流媒体',[icon]='lucide:RadioTower',[sort]=21,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-sessions',[redirect]='',[component]='gb28181/zlm/SessionManagement',[title]=N'会话管理',[icon]='lucide:Users',[sort]=22,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-proxies',[redirect]='',[component]='gb28181/zlm/ProxyManagement',[title]=N'拉流代理',[icon]='lucide:Network',[sort]=30,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-ffmpeg-sources',[redirect]='',[component]='gb28181/zlm/FFmpegSources',[title]=N'FFmpeg 源',[icon]='lucide:Clapperboard',[sort]=31,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-rtp-servers',[redirect]='',[component]='gb28181/zlm/RTPServices',[title]=N'RTP 服务',[icon]='lucide:Waypoints',[sort]=32,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-cloud-recordings',[redirect]='',[component]='gb28181/cloud-recordings/index',[title]=N'录制管理',[icon]='lucide:Cloud',[sort]=40,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-recording-schedules',[redirect]='',[component]='gb28181/recording-schedules/index',[title]=N'录像计划',[icon]='lucide:CalendarClock',[sort]=41,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='gb28181-zlm-config',[redirect]='',[component]='gb28181/zlm/ServerConfig',[title]=N'服务配置',[icon]='lucide:Settings2',[sort]=42,[type]=2,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=0,[name]='gb28181-zlm-node-detail',[redirect]='',[component]='gb28181/zlm/NodeDetail',[title]=N'节点详情',[icon]='lucide:Server',[sort]=6,[type]=2,[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes/:id' AND [deleted_at] IS NULL;

-- media-workbench-v2:down:end
