IF COL_LENGTH(N'dbo.gb_cascade_platform', N'config_enabled') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [config_enabled] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_config_enabled] DEFAULT (0);
END;
