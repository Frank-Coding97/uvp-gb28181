-- 回滚播放鉴权配置 API 权限（SQL Server）。
DELETE FROM [sys_casbin_rule] WHERE [v1]='/api/gb28181/sip/service-config/play-auth' AND [v2] IN ('GET','PUT');
DELETE ma FROM [sys_menu_api] ma JOIN [sys_api] a ON a.[id]=ma.[api_id] WHERE a.[path]='/api/gb28181/sip/service-config/play-auth';
DELETE FROM [sys_api] WHERE [path]='/api/gb28181/sip/service-config/play-auth' AND [method] IN ('GET','PUT');
