IF COL_LENGTH(N'dbo.gb_device_firmware_upgrade', N'cascade_session_key') IS NULL ALTER TABLE [dbo].[gb_device_firmware_upgrade] ADD [cascade_session_key] VARCHAR(64) NULL;
