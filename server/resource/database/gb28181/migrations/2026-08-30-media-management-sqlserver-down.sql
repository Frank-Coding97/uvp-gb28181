-- Roll back media-management menu/API bindings (sqlserver).
DELETE c FROM [sys_casbin_rule] c JOIN [sys_api] a ON c.[v1]=a.[path] AND c.[v2]=a.[method] WHERE a.[api_group]=N'GB28181 媒体管理';
DELETE ma FROM [sys_menu_api] ma JOIN [sys_api] a ON a.[id]=ma.[api_id] WHERE a.[api_group]=N'GB28181 媒体管理';
DELETE rm FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] WHERE m.[path] IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/zlm/config') OR m.[permission] IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart');
DELETE FROM [sys_menu] WHERE [permission] IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart');
DELETE FROM [sys_menu] WHERE [path] IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/zlm/config');
DECLARE @MEDIA_MENU_ID BIGINT;
SELECT TOP 1 @MEDIA_MENU_ID=[id] FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL ORDER BY [id];
UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'流媒体节点',[icon]='lucide:Server',[sort]=10,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'调度算法',[icon]='lucide:Workflow',[sort]=11,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'调度日志',[icon]='lucide:History',[sort]=12,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=0,[redirect]='',[title]=N'云端录像',[icon]='lucide:Cloud',[sort]=35,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=0,[redirect]='',[title]=N'录像计划',[icon]='lucide:CalendarClock',[sort]=34,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [redirect]='',[title]=N'流媒体管理',[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/media' AND [deleted_at] IS NULL;
DELETE FROM [sys_api] WHERE [api_group]=N'GB28181 媒体管理';
