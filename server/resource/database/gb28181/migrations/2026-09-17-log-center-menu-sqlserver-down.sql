-- 回滚「日志中心」：菜单归位、标题还原、目录行移除（SQL Server，幂等）
--
-- 恢复的是 up 执行前的实测值：sip-traces 是一级菜单 sort=90；
--   realtime-log / login-log / log 挂 /system（sort 2 / 1 / 0）；joblog 挂 /sysjobs（sort 0）。
-- 全部按 path 定位；父目录取不到 id 时保护不写 NULL。

UPDATE [sys_menu] SET [parent_id]=0, [sort]=90, [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/gb28181/sip-traces' AND [deleted_at] IS NULL AND [type] IN (1,2) AND ([sort] IS NULL OR [sort]<>90);

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL), [sort]=2, [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/realtime-log' AND [deleted_at] IS NULL AND [type] IN (1,2) AND ([sort] IS NULL OR [sort]<>2)
  AND (SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL) IS NOT NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL), [sort]=1, [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/login-log' AND [deleted_at] IS NULL AND [type] IN (1,2) AND ([sort] IS NULL OR [sort]<>1)
  AND (SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL) IS NOT NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL), [sort]=0, [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/log' AND [deleted_at] IS NULL AND [type] IN (1,2) AND ([sort] IS NULL OR [sort]<>0)
  AND (SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/system' AND [type]=1 AND [deleted_at] IS NULL) IS NOT NULL;

UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/sysjobs' AND [type]=1 AND [deleted_at] IS NULL), [sort]=0, [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/joblog' AND [deleted_at] IS NULL AND [type] IN (1,2) AND ([sort] IS NULL OR [sort]<>0)
  AND (SELECT MIN([id]) FROM [sys_menu] WHERE [path]=N'/sysjobs' AND [type]=1 AND [deleted_at] IS NULL) IS NOT NULL;

UPDATE [sys_menu] SET [title]=N'log', [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/log' AND [deleted_at] IS NULL AND [title]=N'操作日志';

UPDATE [sys_menu] SET [title]=N'joblog', [updated_at]=CURRENT_TIMESTAMP
WHERE [path]=N'/system/joblog' AND [deleted_at] IS NULL AND [title]=N'定时任务日志';

DELETE FROM [sys_role_menu] WHERE [menu_id] IN (SELECT [id] FROM [sys_menu] WHERE [path]=N'/log-center');

DELETE FROM [sys_menu] WHERE [path]=N'/log-center';
