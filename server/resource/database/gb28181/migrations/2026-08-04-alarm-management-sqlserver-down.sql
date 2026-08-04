-- Remove alarm management configuration without deleting gb_alarm_event data.
DELETE FROM [sys_casbin_rule] WHERE [v1] IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete');
DELETE FROM [sys_menu_api] WHERE [api_id] IN (SELECT [id] FROM [sys_api] WHERE [path] IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete'));
DELETE FROM [sys_role_menu] WHERE [menu_id] IN (SELECT [id] FROM [sys_menu] WHERE [path]='/gb28181/alarm-management' OR [permission] IN ('gb28181:alarm:view','gb28181:alarm:delete'));
DELETE FROM [sys_menu] WHERE [path]='/gb28181/alarm-management' OR [permission] IN ('gb28181:alarm:view','gb28181:alarm:delete');
DELETE FROM [sys_api] WHERE [path] IN ('/api/gb28181/alarms','/api/gb28181/alarms/:id','/api/gb28181/alarms/batch-delete');
IF EXISTS (SELECT 1 FROM sys.indexes WHERE object_id=OBJECT_ID(N'gb_alarm_event') AND name=N'idx_alarm_time')
  DROP INDEX [idx_alarm_time] ON [gb_alarm_event];
