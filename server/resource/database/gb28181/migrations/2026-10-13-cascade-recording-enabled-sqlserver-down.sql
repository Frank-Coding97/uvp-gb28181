IF COL_LENGTH(N'dbo.gb_cascade_platform', N'recording_enabled') IS NOT NULL
BEGIN
  DECLARE @cascade_recording_enabled_default NVARCHAR(128);
  SELECT @cascade_recording_enabled_default = dc.name FROM sys.default_constraints dc
  JOIN sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
  WHERE dc.parent_object_id = OBJECT_ID(N'dbo.gb_cascade_platform') AND c.name = N'recording_enabled';
  IF @cascade_recording_enabled_default IS NOT NULL
    EXEC(N'ALTER TABLE [dbo].[gb_cascade_platform] DROP CONSTRAINT ' + QUOTENAME(@cascade_recording_enabled_default));
  ALTER TABLE [dbo].[gb_cascade_platform] DROP COLUMN [recording_enabled];
END;
