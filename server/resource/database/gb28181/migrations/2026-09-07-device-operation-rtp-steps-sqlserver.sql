-- RTP-only fixed evidence. NULL preserves unknown legacy history.
-- NVARCHAR is UTF-16: DB limit is 32768 code units; the application additionally
-- enforces the stricter 32768-byte UTF-8 canonical JSON budget before every write.
IF OBJECT_ID(N'dbo.gb_device_operation_intent', N'U') IS NULL THROW 51000, N'RTP steps require device operation intent', 1;
IF COL_LENGTH(N'dbo.gb_device_operation_intent', N'rtp_steps_json') IS NULL ALTER TABLE dbo.gb_device_operation_intent ADD rtp_steps_json NVARCHAR(MAX) NULL;
IF NOT EXISTS (SELECT 1 FROM sys.check_constraints WHERE parent_object_id = OBJECT_ID(N'dbo.gb_device_operation_intent') AND name = N'ck_device_intent_rtp_size') ALTER TABLE dbo.gb_device_operation_intent ADD CONSTRAINT ck_device_intent_rtp_size CHECK (rtp_steps_json IS NULL OR DATALENGTH(rtp_steps_json) BETWEEN 2 AND 65536);
