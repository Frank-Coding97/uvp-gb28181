-- Device access cleanup barrier (SQL Server 2017+).
-- access_epoch is the requested waterline. A newly added completed waterline
-- intentionally stays at 1, so upgraded rows with access_epoch > 1 remain
-- pending until the cleanup worker records completion.

IF COL_LENGTH(N'dbo.gb_device',N'access_epoch') IS NULL THROW 51000, N'device cleanup barrier requires gb_device.access_epoch', 1;
IF COL_LENGTH(N'dbo.gb_device',N'cleanup_completed_epoch') IS NULL ALTER TABLE dbo.gb_device ADD cleanup_completed_epoch BIGINT NOT NULL CONSTRAINT df_gb_device_cleanup_completed_epoch DEFAULT 1;
IF NOT EXISTS (SELECT 1 FROM sys.check_constraints WHERE parent_object_id = OBJECT_ID(N'dbo.gb_device') AND name = N'ck_gb_device_cleanup_completed_epoch') ALTER TABLE dbo.gb_device ADD CONSTRAINT ck_gb_device_cleanup_completed_epoch CHECK (cleanup_completed_epoch > 0 AND cleanup_completed_epoch <= access_epoch);
