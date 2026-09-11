DELETE FROM [sys_casbin_rule] WHERE [v1] LIKE '/api/gb28181/recording-plans%';
DELETE ma FROM [sys_menu_api] ma JOIN [sys_api] a ON a.[id]=ma.[api_id] WHERE a.[path] LIKE '/api/gb28181/recording-plans%';
DELETE rm FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] WHERE m.[permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign');
DELETE FROM [sys_menu] WHERE [permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign');
DELETE FROM [sys_api] WHERE [path] LIKE '/api/gb28181/recording-plans%';
