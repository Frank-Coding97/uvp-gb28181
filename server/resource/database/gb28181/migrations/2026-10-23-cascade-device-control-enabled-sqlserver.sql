IF COL_LENGTH(N'dbo.gb_cascade_platform', N'device_control_enabled') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [device_control_enabled] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_device_control_enabled] DEFAULT (0);
END;
