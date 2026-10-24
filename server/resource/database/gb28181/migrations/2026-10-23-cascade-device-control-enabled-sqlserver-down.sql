IF COL_LENGTH(N'dbo.gb_cascade_platform', N'device_control_enabled') IS NOT NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] DROP COLUMN [device_control_enabled];
END;
