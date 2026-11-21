IF COL_LENGTH(N'dbo.gb_cascade_platform', N'auto_push_catalog') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [auto_push_catalog] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_auto_push_catalog] DEFAULT (0);
END;
