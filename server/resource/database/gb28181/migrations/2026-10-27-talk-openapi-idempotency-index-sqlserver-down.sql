IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'idx_talk_session_openapi_idempotency' AND object_id = OBJECT_ID(N'dbo.gb_talk_session'))
    DROP INDEX [idx_talk_session_openapi_idempotency] ON [dbo].[gb_talk_session];
