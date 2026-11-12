-- 与模型上的复合索引同名：幂等查询按 (client_id, idempotency_key) 命中。
-- 存量库缺索引时功能正常但每次幂等查询都全表扫，且无法挡住并发重复提交。
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_talk_session_openapi_idempotency' AND object_id = OBJECT_ID(N'dbo.gb_talk_session'))
    CREATE INDEX [idx_talk_session_openapi_idempotency] ON [dbo].[gb_talk_session] ([openapi_client_id], [openapi_idempotency_key]);
