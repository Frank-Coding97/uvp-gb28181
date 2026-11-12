IF COL_LENGTH(N'dbo.gb_cascade_platform', N'auto_push_catalog') IS NOT NULL
BEGIN
  DECLARE @cascade_auto_push_catalog_default NVARCHAR(128);
  SELECT @cascade_auto_push_catalog_default = dc.name FROM sys.default_constraints dc
  JOIN sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
  WHERE dc.parent_object_id = OBJECT_ID(N'dbo.gb_cascade_platform') AND c.name = N'auto_push_catalog';
  IF @cascade_auto_push_catalog_default IS NOT NULL
    EXEC(N'ALTER TABLE [dbo].[gb_cascade_platform] DROP CONSTRAINT ' + QUOTENAME(@cascade_auto_push_catalog_default));
  ALTER TABLE [dbo].[gb_cascade_platform] DROP COLUMN [auto_push_catalog];
END;
