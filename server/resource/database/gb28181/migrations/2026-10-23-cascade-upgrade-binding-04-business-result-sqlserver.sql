IF COL_LENGTH(N'dbo.gb_device_firmware_upgrade', N'cascade_business_result') IS NULL ALTER TABLE [dbo].[gb_device_firmware_upgrade] ADD [cascade_business_result] VARCHAR(16) NULL;
