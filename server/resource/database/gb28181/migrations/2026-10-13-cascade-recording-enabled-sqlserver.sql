IF COL_LENGTH(N'dbo.gb_cascade_platform', N'recording_enabled') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [recording_enabled] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_recording_enabled] DEFAULT (0);
END;
