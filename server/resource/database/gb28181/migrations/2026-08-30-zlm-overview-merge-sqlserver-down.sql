-- zlm-overview-merge:down:start
UPDATE [sys_menu] SET [title]=N'集群总览',[icon]='lucide:LayoutDashboard',[sort]=10,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [title]=N'总览',[icon]='lucide:Gauge',[sort]=30,[hide]=0,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;
-- zlm-overview-merge:down:end
