-- 软删除异步视频探针查询 API 及其绑定，不恢复旧路径。
DELETE FROM sys_casbin_rule WHERE v1='/api/gb28181/stream-probes/operations/:operationId' AND v2='GET';
DELETE FROM sys_menu_api WHERE api_id IN (SELECT id FROM sys_api WHERE path='/api/gb28181/stream-probes/operations/:operationId' AND method='GET');
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
WHERE path='/api/gb28181/stream-probes/operations/:operationId' AND method='GET' AND deleted_at IS NULL;
