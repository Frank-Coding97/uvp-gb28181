IF COL_LENGTH(N'dbo.gb_cascade_platform', N'rtcp_keepalive') IS NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] ADD [rtcp_keepalive] BIT NOT NULL CONSTRAINT [df_gb_cascade_platform_rtcp_keepalive] DEFAULT (0);
END;
