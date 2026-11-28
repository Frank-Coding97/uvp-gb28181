IF COL_LENGTH(N'dbo.gb_device_firmware_upgrade', N'cascade_binding') IS NULL ALTER TABLE [dbo].[gb_device_firmware_upgrade] ADD [cascade_binding] NVARCHAR(MAX) NULL;
