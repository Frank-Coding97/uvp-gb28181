IF COL_LENGTH(N'dbo.gb_device_firmware_upgrade', N'cascade_request_key') IS NULL ALTER TABLE [dbo].[gb_device_firmware_upgrade] ADD [cascade_request_key] VARCHAR(64) NULL;
