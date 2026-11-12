IF COL_LENGTH(N'dbo.gb_cascade_platform', N'maintenance_enabled') IS NOT NULL
BEGIN
  ALTER TABLE [dbo].[gb_cascade_platform] DROP COLUMN [maintenance_enabled];
END;
