-- 回退为 P5 初始业务日志命名（PostgreSQL）
UPDATE sys_menu SET title='实时业务日志',updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/realtime-log' AND deleted_at IS NULL AND title='实时日志控制台';
UPDATE sys_api SET title='实时业务日志流',updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/logs/stream' AND method='GET' AND deleted_at IS NULL AND title='实时控制台日志流';
