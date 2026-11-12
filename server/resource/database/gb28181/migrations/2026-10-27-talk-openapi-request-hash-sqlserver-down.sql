IF COL_LENGTH(N'dbo.gb_talk_session', N'openapi_request_hash') IS NOT NULL ALTER TABLE [dbo].[gb_talk_session] DROP COLUMN [openapi_request_hash];
