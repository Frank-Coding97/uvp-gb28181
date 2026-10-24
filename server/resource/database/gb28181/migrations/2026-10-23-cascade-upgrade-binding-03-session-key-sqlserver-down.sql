IF COL_LENGTH(N'dbo.gb_device_firmware_upgrade', N'cascade_session_key') IS NOT NULL EXEC(N'ALTER TABLE [dbo].[gb_device_firmware_upgrade] DROP COLUMN [cascade_session_key]');
