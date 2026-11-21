-- 与模型上的复合索引同名：幂等查询按 (client_id, idempotency_key) 命中。
-- 存量库缺索引时功能正常但每次幂等查询都全表扫，且无法挡住并发重复提交。
SET @ddl := IF((SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_talk_session' AND INDEX_NAME = 'idx_talk_session_openapi_idempotency') = 0,
  'CREATE INDEX `idx_talk_session_openapi_idempotency` ON `gb_talk_session` (`openapi_client_id`, `openapi_idempotency_key`)', 'SELECT 1');
PREPARE talk_idem_idx FROM @ddl;
EXECUTE talk_idem_idx;
DEALLOCATE PREPARE talk_idem_idx;
