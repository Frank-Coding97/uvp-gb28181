-- 将已落库的 P5 菜单和 API 名称修正为控制台日志语义（SQL Server，幂等）
UPDATE sys_menu SET title=N'实时日志控制台',updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/realtime-log' AND deleted_at IS NULL AND title<>N'实时日志控制台';
UPDATE sys_api SET title=N'实时控制台日志流',updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/logs/stream' AND method='GET' AND deleted_at IS NULL AND title<>N'实时控制台日志流';
