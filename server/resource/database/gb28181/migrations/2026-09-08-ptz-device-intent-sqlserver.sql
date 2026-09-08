-- Original PTZ authorization; historical rows deliberately remain NULL.
IF COL_LENGTH(N'dbo.gb_ptz_operation',N'device_epoch') IS NULL ALTER TABLE dbo.gb_ptz_operation ADD device_epoch BIGINT NULL;
IF COL_LENGTH(N'dbo.gb_ptz_operation',N'device_intent_id') IS NULL ALTER TABLE dbo.gb_ptz_operation ADD device_intent_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NULL;
IF COL_LENGTH(N'dbo.gb_ptz_operation_attempt',N'owner_process_id') IS NULL ALTER TABLE dbo.gb_ptz_operation_attempt ADD owner_process_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NULL;
IF COL_LENGTH(N'dbo.gb_ptz_operation_attempt',N'owner_run_id') IS NULL ALTER TABLE dbo.gb_ptz_operation_attempt ADD owner_run_id VARCHAR(32) COLLATE Latin1_General_100_BIN2 NULL;
IF COL_LENGTH(N'dbo.gb_ptz_operation_attempt',N'local_quiesced_at') IS NULL ALTER TABLE dbo.gb_ptz_operation_attempt ADD local_quiesced_at DATETIME2(6) NULL;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'dbo.gb_ptz_operation') AND name = N'uk_ptz_device_intent') CREATE UNIQUE INDEX uk_ptz_device_intent ON dbo.gb_ptz_operation(device_intent_id) WHERE device_intent_id IS NOT NULL;
