IF COL_LENGTH(N'dbo.gb_cascade_platform', N'maintenance_enabled') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [maintenance_enabled] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_maintenance_enabled] DEFAULT (0);
END;
