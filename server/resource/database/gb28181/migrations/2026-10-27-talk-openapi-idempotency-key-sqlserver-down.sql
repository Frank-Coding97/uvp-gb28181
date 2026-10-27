IF COL_LENGTH(N'dbo.gb_talk_session', N'openapi_idempotency_key') IS NOT NULL ALTER TABLE [dbo].[gb_talk_session] DROP COLUMN [openapi_idempotency_key];
